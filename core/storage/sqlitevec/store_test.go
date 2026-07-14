package sqlitevec

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "vectors.db")
	store, err := Open(context.Background(), path, 3)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

func TestReplaceAndSearchReturnsNearestFirst(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	embeddings := []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 0, 0}},
		{ChunkID: 2, Vector: []float32{0, 1, 0}},
		{ChunkID: 3, Vector: []float32{0, 0, 1}},
	}
	if err := store.Replace(ctx, embeddings); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0.9, 0.1, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	if hits[0].ChunkID != 1 {
		t.Fatalf("want nearest chunk 1, got %d", hits[0].ChunkID)
	}
	if hits[0].Distance > hits[1].Distance {
		t.Fatalf("hits not ordered by distance: %v", hits)
	}
}

func TestReplaceUpsertsExistingChunk(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0, 0}}}); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	// Re-embed chunk 1 to point the other way; the old row must be gone.
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{0, 0, 1}}}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0, 0, 1}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit after upsert, got %d", len(hits))
	}
	if hits[0].Distance > 1e-5 {
		t.Fatalf("upsert did not replace vector, distance %f", hits[0].Distance)
	}
}

func TestDeleteRemovesVectors(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Replace(ctx, []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 0, 0}},
		{ChunkID: 2, Vector: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := store.Delete(ctx, []int64{1}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	hits, err := store.Search(ctx, []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != 2 {
		t.Fatalf("want only chunk 2 remaining, got %v", hits)
	}
}

func TestSearchRejectsDimensionMismatch(t *testing.T) {
	store := openTestStore(t)

	if _, err := store.Search(context.Background(), []float32{1, 0}, 5); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestOpenRejectsBadDimensions(t *testing.T) {
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "v.db"), 0); err == nil {
		t.Fatal("expected an error for zero dimensions")
	}
}

func TestReplaceAndDeleteEmptyAreNoops(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	if err := store.Replace(context.Background(), nil); err != nil {
		t.Fatalf("empty replace: %v", err)
	}
	if err := store.Delete(context.Background(), nil); err != nil {
		t.Fatalf("empty delete: %v", err)
	}
}

func TestReplaceValidatesEmbeddings(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 0, Vector: []float32{1, 0, 0}}}); err == nil {
		t.Fatal("expected an error for a zero chunk id")
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0}}}); err == nil {
		t.Fatal("expected a dimension mismatch error")
	}
}

func TestReplaceHandlesZeroVector(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	if err := store.Replace(context.Background(), []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{0, 0, 0}}}); err != nil {
		t.Fatalf("zero vector replace: %v", err)
	}
}
