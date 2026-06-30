package strategy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"semantic-search/internal/chunker"
	"semantic-search/internal/embedder"
	"semantic-search/internal/parser"
	"semantic-search/internal/reader"
	storage "semantic-search/internal/storage/sqlite"
)

const strategyBatchSize = 1

type Reader interface {
	Read(ctx context.Context, document storage.Document) (string, error)
}

type Parser interface {
	Parse(ctx context.Context, text string) (string, error)
}

type Chunker interface {
	Chunk(ctx context.Context, input chunker.Input) ([]storage.Chunk, error)
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type FileStrategy struct {
	Extensions []string
	Reader     Reader
	Parser     Parser
	Chunker    Chunker
	Embedder   Embedder
}

type Pool []FileStrategy

type Store interface {
	DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error)
	ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error)
	UpdateDocumentStatus(ctx context.Context, fileID string, status string) error
}

type VectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error
}

type Result struct {
	Chunked  int
	Embedded int
}

func DefaultPool() Pool {
	openAIEmbedder := embedder.NewOpenAIEmbedder(embedder.DefaultBaseURL, embedder.DefaultModel)
	return Pool{
		{
			Extensions: []string{".md", ".markdown", ".mdown"},
			Reader:     reader.MarkdownReader{},
			Parser:     parser.MarkdownParser{},
			Chunker:    chunker.NewHardLimitChunker(chunker.DefaultMaxTokens),
			Embedder:   openAIEmbedder,
		},
	}
}

func (p Pool) Supports(path string) bool {
	_, ok := p.Find(path)
	return ok
}

func (p Pool) Find(path string) (FileStrategy, bool) {
	for _, fileStrategy := range p {
		if fileStrategy.Supports(path) {
			return fileStrategy, true
		}
	}

	return FileStrategy{}, false
}

func (s FileStrategy) Supports(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	for _, supportedExtension := range s.Extensions {
		if extension == strings.ToLower(supportedExtension) {
			return true
		}
	}

	return false
}

func (s FileStrategy) Process(ctx context.Context, document storage.Document) ([]storage.Chunk, error) {
	if s.Reader == nil {
		return nil, fmt.Errorf("reader is required")
	}
	if s.Parser == nil {
		return nil, fmt.Errorf("parser is required")
	}
	if s.Chunker == nil {
		return nil, fmt.Errorf("chunker is required")
	}

	text, err := s.Reader.Read(ctx, document)
	if err != nil {
		return nil, err
	}

	parsedText, err := s.Parser.Parse(ctx, text)
	if err != nil {
		return nil, err
	}

	return s.Chunker.Chunk(ctx, chunker.Input{Document: document, Text: parsedText})
}

func ProcessScannedDocuments(ctx context.Context, store Store, vectorStore VectorStore, pool Pool) (Result, error) {
	var result Result

	for {
		documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusScanned, strategyBatchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, nil
		}

		for _, document := range documents {
			fileStrategy, ok := pool.Find(document.AbsolutePath)
			if !ok {
				return result, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
			}

			chunks, err := fileStrategy.Process(ctx, document)
			if err != nil {
				return result, fmt.Errorf("process document %q: %w", document.AbsolutePath, err)
			}

			existingChunks, err := store.ChunksByDocumentID(ctx, document.ID)
			if err != nil {
				return result, fmt.Errorf("load existing chunks for %q: %w", document.AbsolutePath, err)
			}
			if err := vectorStore.Delete(ctx, chunkIDs(existingChunks)); err != nil {
				return result, fmt.Errorf("delete old vectors for %q: %w", document.AbsolutePath, err)
			}

			if err := store.ReplaceDocumentChunksAndStatus(ctx, document.ID, chunks, storage.DocumentStatusChunked); err != nil {
				return result, fmt.Errorf("store chunks for %q: %w", document.AbsolutePath, err)
			}

			result.Chunked++
		}
	}
}

func ProcessChunkedDocuments(ctx context.Context, store Store, vectorStore VectorStore, pool Pool) (Result, error) {
	var result Result

	for {
		documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusChunked, strategyBatchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, nil
		}

		for _, document := range documents {
			fileStrategy, ok := pool.Find(document.AbsolutePath)
			if !ok {
				return result, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
			}
			if fileStrategy.Embedder == nil {
				return result, fmt.Errorf("embedder is required for document %q", document.AbsolutePath)
			}

			chunks, err := store.ChunksByDocumentID(ctx, document.ID)
			if err != nil {
				return result, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
			}
			if len(chunks) == 0 {
				if err := store.UpdateDocumentStatus(ctx, document.FileID, storage.DocumentStatusDone); err != nil {
					return result, fmt.Errorf("mark empty document done %q: %w", document.AbsolutePath, err)
				}
				result.Embedded++
				continue
			}

			texts := make([]string, len(chunks))
			for i, chunk := range chunks {
				texts[i] = chunk.Text
			}

			vectorValues, err := fileStrategy.Embedder.Embed(ctx, texts)
			if err != nil {
				return result, fmt.Errorf("embed chunks for %q: %w", document.AbsolutePath, err)
			}
			if len(vectorValues) != len(chunks) {
				return result, fmt.Errorf("embedding count mismatch for %q: want %d, got %d", document.AbsolutePath, len(chunks), len(vectorValues))
			}

			dimensions := len(vectorValues[0])
			embeddings := make([]storage.ChunkEmbedding, len(chunks))
			for i, chunk := range chunks {
				if len(vectorValues[i]) != dimensions {
					return result, fmt.Errorf("embedding dimension mismatch for %q chunk %d", document.AbsolutePath, chunk.ChunkIndex)
				}
				embeddings[i] = storage.ChunkEmbedding{ChunkID: chunk.ID, Vector: vectorValues[i]}
			}

			if err := vectorStore.Replace(ctx, embeddings); err != nil {
				return result, fmt.Errorf("store vectors for %q: %w", document.AbsolutePath, err)
			}

			if err := store.UpdateDocumentStatus(ctx, document.FileID, storage.DocumentStatusDone); err != nil {
				return result, fmt.Errorf("mark document done %q: %w", document.AbsolutePath, err)
			}

			result.Embedded++
		}
	}
}

func chunkIDs(chunks []storage.Chunk) []int64 {
	ids := make([]int64, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.ID != 0 {
			ids = append(ids, chunk.ID)
		}
	}

	return ids
}
