package pgvector

import (
	"context"
	"errors"
	"os"
	"testing"

	"database/sql"
	"database/sql/driver"
	storage "github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/internal/dbmock"
	_ "github.com/jackc/pgx/v5/stdlib"
)

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

func TestStoreMethodsErrorOnClosedDB(t *testing.T) {
	store := testStore(t, 3, false)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if err := store.EnsureSchema(ctx); err == nil {
		t.Error("EnsureSchema: expected an error on a closed database")
	}
	if err := store.Delete(ctx, []int64{1}); err == nil {
		t.Error("Delete: expected an error on a closed database")
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 0, 0}}}); err == nil {
		t.Error("Replace: expected an error on a closed database")
	}
	if _, err := store.Search(ctx, []float32{1, 0, 0}, 5); err == nil {
		t.Error("Search: expected an error on a closed database")
	}
}

var errMock = errors.New("mock failure")

func mockStore(t *testing.T, config dbmock.Config, dimensions int, hnsw bool) *Store {
	t.Helper()
	db := dbmock.Open(config)
	t.Cleanup(func() { _ = db.Close() })

	return &Store{db: db, dimensions: dimensions, hnsw: hnsw}
}

func withOpenDB(t *testing.T, config dbmock.Config, openErr error) {
	t.Helper()
	original := openDB
	openDB = func(string, string) (*sql.DB, error) {
		if openErr != nil {
			return nil, openErr
		}

		return dbmock.Open(config), nil
	}
	t.Cleanup(func() { openDB = original })
}

func TestOpenRejectsNonPositiveDimensions(t *testing.T) {
	if _, err := Open(context.Background(), "", 0, false); err == nil {
		t.Fatal("want an error for zero dimensions")
	}
}

func TestOpenReportsAConnectionFailure(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, errMock)

	if _, err := Open(context.Background(), "", 8, false); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenReportsASchemaFailureAndClosesTheHandle(t *testing.T) {
	withOpenDB(t, dbmock.Config{Fallback: dbmock.Response{Err: errMock}}, nil)

	if _, err := Open(context.Background(), "", 8, false); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenBuildsAStore(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, nil)

	store, err := Open(context.Background(), "", 8, true)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if store.dimensions != 8 || !store.hnsw {
		t.Fatalf("store not configured: %+v", store)
	}
}

func TestEnsureSchemaReportsEachStepFailure(t *testing.T) {
	ctx := context.Background()

	extension := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{Err: errMock}}}, 8, false)
	if err := extension.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("extension: %v", err)
	}

	table := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{}, {Err: errMock}}}, 8, false)
	if err := table.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("table: %v", err)
	}

	index := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{}, {}, {Err: errMock}}}, 8, true)
	if err := index.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("index: %v", err)
	}
}

func TestEnsureSchemaBuildsTheIndexOnlyInHNSWMode(t *testing.T) {
	ctx := context.Background()

	knn := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{}, {}, {Err: errMock}}}, 8, false)
	if err := knn.EnsureSchema(ctx); err != nil {
		t.Fatalf("knn: %v", err)
	}

	hnsw := mockStore(t, dbmock.Config{}, 8, true)
	if err := hnsw.EnsureSchema(ctx); err != nil {
		t.Fatalf("hnsw: %v", err)
	}
}

func TestDeleteSkipsAnEmptyListAndReportsFailure(t *testing.T) {
	ctx := context.Background()

	store := mockStore(t, dbmock.Config{Fallback: dbmock.Response{Err: errMock}}, 8, false)
	if err := store.Delete(ctx, nil); err != nil {
		t.Fatalf("empty delete should be a no-op: %v", err)
	}
	if err := store.Delete(ctx, []int64{1, 2}); !errors.Is(err, errMock) {
		t.Fatalf("delete: %v", err)
	}

	ok := mockStore(t, dbmock.Config{}, 8, false)
	if err := ok.Delete(ctx, []int64{1}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestReplaceValidatesBeforeTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	store := mockStore(t, dbmock.Config{}, 3, false)

	if err := store.Replace(ctx, nil); err != nil {
		t.Fatalf("empty replace should be a no-op: %v", err)
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{Vector: []float32{1, 2, 3}}}); err == nil {
		t.Fatal("want an error for a missing chunk id")
	}
	if err := store.Replace(ctx, []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1}}}); err == nil {
		t.Fatal("want an error for a dimension mismatch")
	}
}

func TestReplaceReportsTransactionFailures(t *testing.T) {
	ctx := context.Background()
	embeddings := []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 2, 3}}}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock}, 3, false)
	if err := begin.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}

	prepare := mockStore(t, dbmock.Config{PrepareErr: errMock}, 3, false)
	if err := prepare.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("prepare: %v", err)
	}

	insert := mockStore(t, dbmock.Config{Fallback: dbmock.Response{Err: errMock}}, 3, false)
	if err := insert.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("insert: %v", err)
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock}, 3, false)
	if err := commit.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}
}

func TestReplaceStoresEveryEmbedding(t *testing.T) {
	store := mockStore(t, dbmock.Config{}, 3, false)

	err := store.Replace(context.Background(), []storage.ChunkEmbedding{
		{ChunkID: 1, Vector: []float32{1, 2, 3}},
		{ChunkID: 2, Vector: []float32{4, 5, 6}},
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
}

func TestSearchGuardsLimitAndDimensions(t *testing.T) {
	ctx := context.Background()
	store := mockStore(t, dbmock.Config{}, 3, false)

	hits, err := store.Search(ctx, []float32{1, 2, 3}, 0)
	if err != nil || hits != nil {
		t.Fatalf("non-positive limit should return nothing: %v %v", hits, err)
	}
	if _, err := store.Search(ctx, []float32{1}, 5); err == nil {
		t.Fatal("want an error for a dimension mismatch")
	}
}

func TestSearchReturnsHitsInOrder(t *testing.T) {
	store := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"chunk_id", "distance"},
		Values:  [][]driver.Value{{int64(7), 0.25}, {int64(9), 0.75}},
	}}, 3, false)

	hits, err := store.Search(context.Background(), []float32{1, 2, 3}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(hits) != 2 || hits[0].ChunkID != 7 || hits[1].ChunkID != 9 {
		t.Fatalf("got %+v", hits)
	}
	if hits[0].Distance != 0.25 {
		t.Fatalf("distance: %v", hits[0].Distance)
	}
}

func TestSearchReportsQueryScanAndIterationFailures(t *testing.T) {
	ctx := context.Background()
	query := []float32{1, 2, 3}

	failed := mockStore(t, dbmock.Config{Fallback: dbmock.Response{Err: errMock}}, 3, false)
	if _, err := failed.Search(ctx, query, 5); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"chunk_id", "distance"},
		Values:  [][]driver.Value{{"not-an-id", 0.5}},
	}}, 3, false)
	if _, err := mistyped.Search(ctx, query, 5); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"chunk_id", "distance"},
		Values:  [][]driver.Value{{int64(1), 0.5}},
		IterErr: errMock,
	}}, 3, false)
	if _, err := broken.Search(ctx, query, 5); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestCloseReleasesTheHandle(t *testing.T) {
	store := &Store{db: dbmock.Open(dbmock.Config{})}

	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestFormatVectorRendersPgvectorText(t *testing.T) {
	if got := formatVector([]float32{0.5, -1, 2}); got != "[0.5,-1,2]" {
		t.Fatalf("got %q", got)
	}
	if got := formatVector(nil); got != "[]" {
		t.Fatalf("got %q", got)
	}
}

func TestInQueryNumbersItsPlaceholders(t *testing.T) {
	query, args := inQuery("DELETE FROM t WHERE id IN (", []int64{4, 5})

	if query != "DELETE FROM t WHERE id IN ($1, $2)" {
		t.Fatalf("got %q", query)
	}
	if len(args) != 2 || args[0] != int64(4) || args[1] != int64(5) {
		t.Fatalf("got %v", args)
	}
}

func TestOpenVectorStorageOpensTheStore(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, nil)

	store, err := OpenVectorStorage(context.Background(), "", 8, true)
	if err != nil {
		t.Fatalf("open vector storage: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("want a store")
	}
}

func TestOpenVectorStorageReportsAFailure(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, errMock)

	store, err := OpenVectorStorage(context.Background(), "", 8, false)
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
	if store != nil {
		t.Fatal("a failure must yield a nil storage.VectorStorage, not a typed nil")
	}
}
