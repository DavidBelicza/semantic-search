package chunker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/storage"
)

func TestEstimateTokenCountUsesAverageTokenLength(t *testing.T) {
	if got := EstimateTokenCount("abcdefg", 3); got != 3 {
		t.Fatalf("estimated token count mismatch: want 3, got %d", got)
	}
}

func TestHardLimitChunkerCutsByEstimatedTokenLimit(t *testing.T) {
	chunker := HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1}
	chunks, err := chunker.Chunk(context.Background(), Input{Text: "abcdefg"})
	if err != nil {
		t.Fatalf("chunk text: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}

	wantTexts := []string{"abc", "def", "g"}
	for i, want := range wantTexts {
		if chunks[i].Text != want {
			t.Fatalf("chunk %d text mismatch: want %q, got %q", i, want, chunks[i].Text)
		}
		if chunks[i].TokenCount > 3 {
			t.Fatalf("chunk %d exceeds token limit: %d", i, chunks[i].TokenCount)
		}
		if chunks[i].ContentHash == "" {
			t.Fatalf("chunk %d hash was not set", i)
		}
	}
}

func TestProcessScannedDocumentsStoresChunksAndMarksChunked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := &memoryChunkStore{
		documents: []storage.Document{
			{
				ID:           42,
				FileID:       "1:100",
				AbsolutePath: path,
				Status:       storage.DocumentStatusScanned,
			},
		},
	}
	strategy := HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1}

	result, err := ProcessScannedDocuments(context.Background(), store, strategy)
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

type memoryChunkStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
}

func (s *memoryChunkStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
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

func (s *memoryChunkStore) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error {
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
