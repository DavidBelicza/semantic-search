package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/davidbelicza/semantic-search/internal/storage/sqlite"
	"github.com/davidbelicza/semantic-search/internal/strategy"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestProcessScannedReconcilesEmbedsAndMarks(t *testing.T) {
	path := writeFile(t, "abcdefg")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{42: {{ID: 99, DocumentID: 42, ChunkIndex: 0, Text: "old", ContentHash: "old"}}},
		nextID:    100,
	}
	vectorStore := &memoryVectorStore{}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, vectorStore, pool, false); err != nil {
		t.Fatalf("process: %v", err)
	}

	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 3 {
		t.Fatalf("stored chunk count mismatch: %d", len(store.chunks[42]))
	}
	if len(vectorStore.deleted) != 1 || vectorStore.deleted[0] != 99 {
		t.Fatalf("deleted vectors mismatch: %#v", vectorStore.deleted)
	}
	if len(vectorStore.embeddings) != 3 {
		t.Fatalf("embedding count mismatch: %d", len(vectorStore.embeddings))
	}
}

func TestProcessScannedLeavesChunkedWhenEmbeddingFails(t *testing.T) {
	path := writeFile(t, "abc")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		nextID:    100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3, embedErr: errors.New("boom")})

	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false); err == nil {
		t.Fatal("expected embedding error")
	}
	if store.documents[0].Status != storage.DocumentStatusChunked {
		t.Fatalf("status mismatch: want chunked, got %q", store.documents[0].Status)
	}
}

func TestProcessChunkedEmbedsAndMarks(t *testing.T) {
	path := writeFile(t, "abc")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusChunked}},
		chunks: map[int64][]storage.Chunk{42: {
			{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "h1"},
			{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "h2"},
		}},
	}
	vectorStore := &memoryVectorStore{}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, vectorStore, pool, false); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(vectorStore.embeddings) != 2 {
		t.Fatalf("embedding count mismatch: %d", len(vectorStore.embeddings))
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: %q", store.documents[0].Status)
	}
}

func TestProcessScannedContinuesAfterErrorWhenNotFailFast(t *testing.T) {
	good := writeFile(t, "abcdef")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 1, FileID: "1:1", AbsolutePath: "/does/not/exist.md", Status: storage.DocumentStatusScanned},
			{ID: 2, FileID: "1:2", AbsolutePath: good, Status: storage.DocumentStatusScanned},
		},
		nextID: 100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false); err == nil {
		t.Fatal("expected an aggregated error for the missing file")
	}
	if store.documents[1].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("valid document was not processed: %q", store.documents[1].Status)
	}
}

// --- test doubles ---

type fakeStrategy struct {
	maxRunes int
	embedErr error
}

func (fakeStrategy) Claims(string) bool { return true }
func (fakeStrategy) CreateMetadata(strategy.FileRef) (storage.FileMetadata, error) {
	return storage.FileMetadata{}, nil
}
func (fakeStrategy) Fingerprint([]byte) string            { return "" }
func (fakeStrategy) Parse(content []byte) (string, error) { return string(content), nil }

func (s fakeStrategy) Chunk(_ storage.Document, text string) ([]storage.Chunk, error) {
	runes := []rune(text)
	var chunks []storage.Chunk
	for start := 0; start < len(runes); start += s.maxRunes {
		end := start + s.maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		piece := string(runes[start:end])
		sum := sha256.Sum256([]byte(piece))
		chunks = append(chunks, storage.Chunk{ChunkIndex: len(chunks), Text: piece, ContentHash: hex.EncodeToString(sum[:])})
	}
	return chunks, nil
}

func (s fakeStrategy) Embed(_ context.Context, chunks []storage.Chunk) ([][]float32, error) {
	if s.embedErr != nil {
		return nil, s.embedErr
	}
	vectors := make([][]float32, len(chunks))
	for i := range chunks {
		vectors[i] = []float32{float32(i)}
	}
	return vectors, nil
}

type memoryStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
	nextID    int64
}

func (s *memoryStore) DocumentsByStatus(_ context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
	var out []storage.Document
	for _, doc := range s.documents {
		if doc.Status != status || doc.ID <= afterID {
			continue
		}
		out = append(out, doc)
		if len(out) == limit {
			return out, nil
		}
	}
	return out, nil
}

func (s *memoryStore) ApplyDocumentChunkReconcile(_ context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	if s.chunks == nil {
		s.chunks = map[int64][]storage.Chunk{}
	}
	removed := map[int64]struct{}{}
	for _, id := range plan.RemoveIDs {
		removed[id] = struct{}{}
	}
	var kept []storage.Chunk
	for _, chunk := range s.chunks[documentID] {
		if _, ok := removed[chunk.ID]; !ok {
			kept = append(kept, chunk)
		}
	}
	inserted := make([]storage.Chunk, len(plan.Insert))
	for i, chunk := range plan.Insert {
		if s.nextID == 0 {
			s.nextID = 1
		}
		chunk.ID = s.nextID
		chunk.DocumentID = documentID
		s.nextID++
		inserted[i] = chunk
		kept = append(kept, chunk)
	}
	s.chunks[documentID] = kept
	return inserted, nil
}

func (s *memoryStore) ChunksByDocumentID(_ context.Context, documentID int64) ([]storage.Chunk, error) {
	return s.chunks[documentID], nil
}

func (s *memoryStore) UpdateDocumentStatus(_ context.Context, fileID string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].Status = status
		}
	}
	return nil
}

func (s *memoryStore) MarkDocumentEmbedded(_ context.Context, fileID string, contentHash string) error {
	for i := range s.documents {
		if s.documents[i].FileID != fileID {
			continue
		}
		s.documents[i].Status = storage.DocumentStatusEmbedded
		s.documents[i].EmbeddedContentHash = contentHash
	}
	return nil
}

type memoryVectorStore struct {
	deleted    []int64
	embeddings []storage.ChunkEmbedding
}

func (s *memoryVectorStore) Delete(_ context.Context, chunkIDs []int64) error {
	s.deleted = append(s.deleted, chunkIDs...)
	return nil
}

func (s *memoryVectorStore) Replace(_ context.Context, embeddings []storage.ChunkEmbedding) error {
	s.embeddings = append(s.embeddings, embeddings...)
	return nil
}
