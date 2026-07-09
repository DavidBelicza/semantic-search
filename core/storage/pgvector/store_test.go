package pgvector

import (
	"context"
	"os"
	"testing"

	storage "github.com/davidbelicza/semantic-search/core/storage"
)

// testStore opens a store against SEMANTIC_SEARCH_POSTGRES_DSN and resets its table. The test
// is skipped when the DSN is not set.
func testStore(t *testing.T, dimensions int) *Store {
	t.Helper()
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run pgvector integration tests")
	}

	store, err := Open(context.Background(), dsn, dimensions)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+chunkVectorsTable); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}

	return store
}

func TestPgvectorReplaceSearchDelete(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 4)

	if err := store.Replace(ctx, []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 0, 0, 0}},
		{ChunkID: 2, Vector: []float32{0, 1, 0, 0}},
		{ChunkID: 3, Vector: []float32{0, 0, 1, 0}},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0.9, 0.1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 || hits[0].ChunkID != 1 {
		t.Fatalf("want chunk 1 as nearest, got %+v", hits)
	}

	if err := store.Delete(ctx, []int64{1}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, err = store.Search(ctx, []float32{0.9, 0.1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	for _, hit := range hits {
		if hit.ChunkID == 1 {
			t.Fatalf("chunk 1 should have been deleted, got %+v", hits)
		}
	}
}

func TestPgvectorReplaceIsUpsert(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 4)

	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{0, 0, 0, 1}}}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0, 0, 0, 1}, 1)
	if err != nil || len(hits) != 1 || hits[0].ChunkID != 1 {
		t.Fatalf("upsert not applied: %v %+v", err, hits)
	}
}
