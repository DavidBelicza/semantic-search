package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

type fakeCleanupStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
	deleted   []int64
}

func (f *fakeCleanupStore) DocumentsFromID(_ context.Context, afterID int64, limit int) ([]storage.Document, error) {
	out := make([]storage.Document, 0, limit)
	for _, d := range f.documents {
		if d.ID > afterID && len(out) < limit {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeCleanupStore) ChunksByDocumentID(_ context.Context, documentID int64) ([]storage.Chunk, error) {
	return f.chunks[documentID], nil
}

func (f *fakeCleanupStore) DeleteDocument(_ context.Context, documentID int64) error {
	f.deleted = append(f.deleted, documentID)
	return nil
}

type fakeCleanupVectorStore struct{ deleted []int64 }

func (f *fakeCleanupVectorStore) Delete(_ context.Context, chunkIDs []int64) error {
	f.deleted = append(f.deleted, chunkIDs...)
	return nil
}

func TestCleanupDeletesOnlyMissingFiles(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.md")
	if err := os.WriteFile(present, []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.md")

	store := &fakeCleanupStore{
		documents: []storage.Document{
			{ID: 1, AbsolutePath: present},
			{ID: 2, AbsolutePath: missing},
		},
		chunks: map[int64][]storage.Chunk{
			1: {{ID: 10}},
			2: {{ID: 20}, {ID: 21}},
		},
	}
	vectors := &fakeCleanupVectorStore{}

	if err := Cleanup(context.Background(), store, vectors, true); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if len(store.deleted) != 1 || store.deleted[0] != 2 {
		t.Fatalf("expected only document 2 deleted, got %v", store.deleted)
	}
	if len(vectors.deleted) != 2 || vectors.deleted[0] != 20 || vectors.deleted[1] != 21 {
		t.Fatalf("expected the missing document's vectors deleted, got %v", vectors.deleted)
	}
}
