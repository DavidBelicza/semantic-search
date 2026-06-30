package strategy

import (
	"context"
	"errors"
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
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error)
	ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error)
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error)
	UpdateDocumentStatus(ctx context.Context, fileID string, status string) error
	MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error
}

type VectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error
}

type Result struct {
	Processed int
	Embedded  int
}

func DefaultPool() Pool {
	openAIEmbedder := embedder.NewOpenAIEmbedder(embedder.DefaultBaseURL, embedder.DefaultModel)
	openAIEmbedder.Dimensions = embedder.DefaultDimensions
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

type documentProcessor func(ctx context.Context, document storage.Document) (bool, error)

// processDocumentsByStatus walks every document in a status, one page at a time,
// ordered by ascending id. With failFast unset, a per-document error is collected and
// processing continues; the joined errors are returned at the end. Paginating by id
// means a document that failed (and so kept its status) is not revisited this run.
func processDocumentsByStatus(ctx context.Context, store Store, status string, failFast bool, process documentProcessor) (Result, error) {
	var result Result
	var errs []error
	var afterID int64

	for {
		documents, err := store.DocumentsByStatus(ctx, status, afterID, strategyBatchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			embedded, err := process(ctx, document)
			if err != nil && failFast {
				return result, err
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if embedded {
				result.Embedded++
			}
			result.Processed++
		}
	}
}

func ProcessScannedDocuments(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, failFast bool) (Result, error) {
	return processDocumentsByStatus(ctx, store, storage.DocumentStatusScanned, failFast, func(ctx context.Context, document storage.Document) (bool, error) {
		return processScannedDocument(ctx, store, vectorStore, pool, document)
	})
}

func ProcessChunkedDocuments(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, failFast bool) (Result, error) {
	return processDocumentsByStatus(ctx, store, storage.DocumentStatusChunked, failFast, func(ctx context.Context, document storage.Document) (bool, error) {
		return embedChunkedDocument(ctx, store, vectorStore, pool, document)
	})
}

// RebuildVectors re-embeds the chunks of every embedded document and replaces their
// vectors, rebuilding the LanceDB index from SQLite (the source of truth). Use it to
// repair drift between the two stores or to recreate a lost vector database.
func RebuildVectors(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, failFast bool) (Result, error) {
	return processDocumentsByStatus(ctx, store, storage.DocumentStatusEmbedded, failFast, func(ctx context.Context, document storage.Document) (bool, error) {
		return rebuildDocumentVectors(ctx, store, vectorStore, pool, document)
	})
}

func rebuildDocumentVectors(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, document storage.Document) (bool, error) {
	fileStrategy, ok := pool.Find(document.AbsolutePath)
	if !ok {
		return false, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return false, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}

	return embedIfNeeded(ctx, vectorStore, fileStrategy, document, chunks)
}

func embedChunkedDocument(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, document storage.Document) (bool, error) {
	fileStrategy, ok := pool.Find(document.AbsolutePath)
	if !ok {
		return false, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return false, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}

	embedded, err := embedIfNeeded(ctx, vectorStore, fileStrategy, document, chunks)
	if err != nil {
		return false, err
	}

	if err := store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return false, fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}

	return embedded, nil
}

func processScannedDocument(ctx context.Context, store Store, vectorStore VectorStore, pool Pool, document storage.Document) (bool, error) {
	fileStrategy, ok := pool.Find(document.AbsolutePath)
	if !ok {
		return false, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := fileStrategy.Process(ctx, document)
	if err != nil {
		return false, fmt.Errorf("process document %q: %w", document.AbsolutePath, err)
	}

	existingChunks, err := store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return false, fmt.Errorf("load existing chunks for %q: %w", document.AbsolutePath, err)
	}

	plan := storage.ReconcileChunks(existingChunks, chunks)

	insertedChunks, err := store.ApplyDocumentChunkReconcile(ctx, document.ID, plan)
	if err != nil {
		return false, fmt.Errorf("reconcile chunks for %q: %w", document.AbsolutePath, err)
	}

	// SQLite is the source of truth: commit the chunk rows before mutating LanceDB, so
	// a crash cannot leave chunk rows pointing at vectors that were already deleted.
	if err := vectorStore.Delete(ctx, plan.RemoveIDs); err != nil {
		return false, fmt.Errorf("delete old vectors for %q: %w", document.AbsolutePath, err)
	}

	if err := store.UpdateDocumentStatus(ctx, document.FileID, storage.DocumentStatusChunked); err != nil {
		return false, fmt.Errorf("mark document chunked %q: %w", document.AbsolutePath, err)
	}

	chunksToEmbed, err := chunksForEmbedding(ctx, store, document, insertedChunks, existingChunks)
	if err != nil {
		return false, err
	}

	embedded, err := embedIfNeeded(ctx, vectorStore, fileStrategy, document, chunksToEmbed)
	if err != nil {
		return false, err
	}

	if err := store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return false, fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}

	return embedded, nil
}

// chunksForEmbedding selects which chunks still need vectors. A document that was
// already embedded keeps valid vectors for its unchanged (kept) chunks, so only the
// newly inserted chunks need embedding — this avoids re-embedding an entire document
// when only its file metadata changed. A document that has never been embedded must
// embed all of its current chunks, even when reconciliation kept them, because no
// vectors exist for them yet.
func chunksForEmbedding(ctx context.Context, store Store, document storage.Document, inserted []storage.Chunk, existing []storage.Chunk) ([]storage.Chunk, error) {
	if document.EmbeddedContentHash != "" {
		return inserted, nil
	}
	if len(existing) == 0 {
		return inserted, nil
	}

	chunks, err := store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return nil, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}

	return chunks, nil
}

func embedIfNeeded(ctx context.Context, vectorStore VectorStore, fileStrategy FileStrategy, document storage.Document, chunks []storage.Chunk) (bool, error) {
	if len(chunks) == 0 {
		return false, nil
	}
	if fileStrategy.Embedder == nil {
		return false, fmt.Errorf("embedder is required for document %q", document.AbsolutePath)
	}
	if err := embedChunks(ctx, vectorStore, fileStrategy.Embedder, document, chunks); err != nil {
		return false, err
	}

	return true, nil
}

func embedChunks(ctx context.Context, vectorStore VectorStore, embedder Embedder, document storage.Document, chunks []storage.Chunk) error {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}

	vectorValues, err := embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed chunks for %q: %w", document.AbsolutePath, err)
	}
	if len(vectorValues) != len(chunks) {
		return fmt.Errorf("embedding count mismatch for %q: want %d, got %d", document.AbsolutePath, len(chunks), len(vectorValues))
	}

	dimensions := len(vectorValues[0])
	embeddings := make([]storage.ChunkEmbedding, len(chunks))
	for i, chunk := range chunks {
		if len(vectorValues[i]) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for %q chunk %d", document.AbsolutePath, chunk.ChunkIndex)
		}
		embeddings[i] = storage.ChunkEmbedding{ChunkID: chunk.ID, Vector: vectorValues[i]}
	}

	if err := vectorStore.Replace(ctx, embeddings); err != nil {
		return fmt.Errorf("store vectors for %q: %w", document.AbsolutePath, err)
	}

	return nil
}
