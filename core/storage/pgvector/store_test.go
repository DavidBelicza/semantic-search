package pgvector

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	storage "github.com/davidbelicza/semantic-search/core/storage"
)

// testStore opens a store against SEMANTIC_SEARCH_POSTGRES_DSN and resets its table. The test
// is skipped when the DSN is not set.
func testStore(t *testing.T, dimensions int, hnsw bool) *Store {
	t.Helper()
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run pgvector integration tests")
	}

	if _, err := reset(context.Background(), dsn); err != nil {
		t.Fatalf("reset: %v", err)
	}

	store, err := Open(context.Background(), dsn, dimensions, hnsw)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

// reset drops the vector table so each test starts from a clean schema (including any index).
func reset(ctx context.Context, dsn string) (bool, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return false, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+chunkVectorsTable)
	return err == nil, err
}

func TestPgvectorReplaceSearchDelete(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 4, false)

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
	store := testStore(t, 4, false)

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

// TestPgvectorHNSWSearch exercises the HNSW index path: the schema builds the index and search
// still returns the nearest vector.
func TestPgvectorHNSWSearch(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 4, true)

	if err := store.Replace(ctx, []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 0, 0, 0}},
		{ChunkID: 2, Vector: []float32{0, 1, 0, 0}},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := store.Search(ctx, []float32{0.9, 0.1, 0, 0}, 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != 1 {
		t.Fatalf("want chunk 1 as nearest, got %+v", hits)
	}
}

func TestPgvectorOpenRejectsBadDimensions(t *testing.T) {
	if os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN") == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN")
	}
	if _, err := Open(context.Background(), os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN"), 0, false); err == nil {
		t.Fatal("expected an error for zero dimensions")
	}
}

func TestPgvectorReplaceAndDeleteEdges(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 3, false)

	if err := store.Replace(ctx, nil); err != nil {
		t.Fatalf("empty replace: %v", err)
	}
	if err := store.Delete(ctx, nil); err != nil {
		t.Fatalf("empty delete: %v", err)
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 0, Vector: []float32{1, 0, 0}}}); err == nil {
		t.Fatal("expected an error for a zero chunk id")
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0}}}); err == nil {
		t.Fatal("expected a dimension mismatch error")
	}
}

func TestPgvectorMethodsErrorOnClosedStore(t *testing.T) {
	ctx := context.Background()
	store := testStore(t, 3, false)
	store.Close()

	if _, err := store.Search(ctx, []float32{1, 0, 0}, 5); err == nil {
		t.Fatal("expected error: Search on closed store")
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0, 0}}}); err == nil {
		t.Fatal("expected error: Replace on closed store")
	}
	if err := store.Delete(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: Delete on closed store")
	}
}
