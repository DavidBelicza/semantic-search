package strategy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"semantic-search/internal/chunker"
	"semantic-search/internal/parser"
	"semantic-search/internal/reader"
	"semantic-search/internal/storage"
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

type FileStrategy struct {
	Extensions []string
	Reader     Reader
	Parser     Parser
	Chunker    Chunker
}

type Pool []FileStrategy

type Store interface {
	DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error)
	ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error
}

type Result struct {
	Chunked int
}

func DefaultPool() Pool {
	return Pool{
		{
			Extensions: []string{".md", ".markdown", ".mdown"},
			Reader:     reader.MarkdownReader{},
			Parser:     parser.MarkdownParser{},
			Chunker:    chunker.NewHardLimitChunker(chunker.DefaultMaxTokens),
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

func ProcessScannedDocuments(ctx context.Context, store Store, pool Pool) (Result, error) {
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

			if err := store.ReplaceDocumentChunksAndStatus(ctx, document.ID, chunks, storage.DocumentStatusChunked); err != nil {
				return result, fmt.Errorf("store chunks for %q: %w", document.AbsolutePath, err)
			}

			result.Chunked++
		}
	}
}
