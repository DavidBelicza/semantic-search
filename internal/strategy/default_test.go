package strategy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/chunker"
	"semantic-search/internal/storage"
)

func TestDefaultPoolSupportsMarkdownOnly(t *testing.T) {
	pool := DefaultPool()
	for _, path := range []string{"note.md", "note.markdown", "note.mdown", "NOTE.MD"} {
		if !pool.Supports(path) {
			t.Fatalf("expected default pool to support %q", path)
		}
	}

	if pool.Supports("note.txt") {
		t.Fatal("expected default pool to reject note.txt")
	}
}

func TestFileStrategyProcessesWithReaderParserAndChunker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fileStrategy, ok := DefaultPool().Find(path)
	if !ok {
		t.Fatal("expected markdown strategy")
	}
	fileStrategy.Chunker = chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1}

	chunks, err := fileStrategy.Process(context.Background(), storage.Document{AbsolutePath: path})
	if err != nil {
		t.Fatalf("process file: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}
}

func TestProcessScannedDocumentsStoresChunksAndMarksChunked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:           42,
				FileID:       "1:100",
				AbsolutePath: path,
				Status:       storage.DocumentStatusScanned,
			},
		},
	}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abcdefg"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
		},
	}

	result, err := ProcessScannedDocuments(context.Background(), store, pool)
	if err != nil {
		t.Fatalf("process scanned documents: %v", err)
	}

	if result.Chunked != 1 {
		t.Fatalf("chunked count mismatch: want 1, got %d", result.Chunked)
	}
	if store.documents[0].Status != storage.DocumentStatusChunked {
		t.Fatalf("status mismatch: want chunked, got %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 3 {
		t.Fatalf("stored chunk count mismatch: want 3, got %d", len(store.chunks[42]))
	}
}

type fakeReader struct {
	text string
}

func (r fakeReader) Read(ctx context.Context, document storage.Document) (string, error) {
	return r.text, nil
}

type fakeParser struct{}

func (p fakeParser) Parse(ctx context.Context, text string) (string, error) {
	return text, nil
}

type memoryStrategyStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
}

func (s *memoryStrategyStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
	var documents []storage.Document
	for _, document := range s.documents {
		if document.Status == status {
			documents = append(documents, document)
			if len(documents) == limit {
				return documents, nil
			}
		}
	}

	return documents, nil
}

func (s *memoryStrategyStore) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error {
	if s.chunks == nil {
		s.chunks = map[int64][]storage.Chunk{}
	}
	s.chunks[documentID] = chunks

	for i := range s.documents {
		if s.documents[i].ID == documentID {
			s.documents[i].Status = status
		}
	}

	return nil
}
