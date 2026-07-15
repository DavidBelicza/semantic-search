package pipeline

import (
	"context"
	"errors"
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

type errListStore struct{ fakeCleanupStore }

func (errListStore) DocumentsFromID(context.Context, int64, int) ([]storage.Document, error) {
	return nil, errors.New("list")
}

type errDeleteStore struct{ fakeCleanupStore }

func (errDeleteStore) DeleteDocument(context.Context, int64) error { return errors.New("delete") }

type errDeleteVectors struct{}

func (errDeleteVectors) Delete(context.Context, []int64) error { return errors.New("vectors") }

type errChunksStore struct{ fakeCleanupStore }

func (e *errChunksStore) ChunksByDocumentID(context.Context, int64) ([]storage.Chunk, error) {
	return nil, errors.New("chunks lookup failed")
}

func TestCleanupReportsChunkLookupError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.md")
	store := &errChunksStore{fakeCleanupStore{
		documents: []storage.Document{{ID: 1, AbsolutePath: missing}},
	}}
	if err := Cleanup(context.Background(), store, &fakeCleanupVectorStore{}, true); err == nil {
		t.Fatal("expected a chunk-lookup error")
	}
}

func TestCleanupReportsAmbiguousStatError(t *testing.T) {
	// A NUL byte in the path makes os.Stat fail with EINVAL on every platform, which is not
	// os.IsNotExist, so the document is left untouched and the error surfaces.
	badPath := filepath.Join(t.TempDir(), "bad\x00name.md")
	store := &fakeCleanupStore{documents: []storage.Document{{ID: 1, AbsolutePath: badPath}}}

	if err := Cleanup(context.Background(), store, &fakeCleanupVectorStore{}, true); err == nil {
		t.Fatal("expected an ambiguous stat error")
	}
	if len(store.deleted) != 0 {
		t.Fatalf("a document with an ambiguous stat must not be deleted, got %v", store.deleted)
	}
}

func TestCleanupPropagatesErrors(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "gone.md") // never created, so the file is missing

	if err := Cleanup(ctx, &errListStore{}, &fakeCleanupVectorStore{}, false); err == nil {
		t.Fatal("expected a list error")
	}

	missingDoc := func() *fakeCleanupStore {
		return &fakeCleanupStore{
			documents: []storage.Document{{ID: 1, AbsolutePath: missing}},
			chunks:    map[int64][]storage.Chunk{1: {{ID: 10}}},
		}
	}

	if err := Cleanup(ctx, missingDoc(), errDeleteVectors{}, true); err == nil {
		t.Fatal("expected a vector-delete error with failFast")
	}
	if err := Cleanup(ctx, &errDeleteStore{*missingDoc()}, &fakeCleanupVectorStore{}, false); err == nil {
		t.Fatal("expected a document-delete error collected without failFast")
	}
}
