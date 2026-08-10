package sqlitevec

import (
	"context"
	"errors"
	"testing"

	"database/sql"
	"database/sql/driver"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/internal/dbmock"
	"path/filepath"
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

func TestSqlitevecMethodsErrorOnClosedStore(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
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
	store := openTestStore(t)
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

func mockStore(t *testing.T, config dbmock.Config, dimensions int) *Store {
	t.Helper()
	db := dbmock.Open(config)
	t.Cleanup(func() { _ = db.Close() })

	return &Store{db: db, dimensions: dimensions}
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

func withFailingSerializer(t *testing.T) {
	t.Helper()
	original := serializeVector
	serializeVector = func([]float32) ([]byte, error) { return nil, errMock }
	t.Cleanup(func() { serializeVector = original })
}

func TestOpenReportsAConnectionFailure(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, errMock)

	if _, err := Open(context.Background(), "x.db", 8); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenReportsASchemaFailureAndClosesTheHandle(t *testing.T) {
	withOpenDB(t, dbmock.Config{Fallback: dbmock.Response{Err: errMock}}, nil)

	if _, err := Open(context.Background(), "x.db", 8); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestDSNAppendsTheBusyTimeoutToEitherFormOfPath(t *testing.T) {
	if got := dsn("/tmp/a.db"); got != "/tmp/a.db?_busy_timeout=5000" {
		t.Fatalf("got %q", got)
	}
	if got := dsn("/tmp/a.db?cache=shared"); got != "/tmp/a.db?cache=shared&_busy_timeout=5000" {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceReportsTransactionFailures(t *testing.T) {
	ctx := context.Background()
	embeddings := []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 2, 3}}}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock}, 3)
	if err := begin.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}

	del := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{Err: errMock}}}, 3)
	if err := del.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("delete: %v", err)
	}

	insert := mockStore(t, dbmock.Config{Responses: []dbmock.Response{{}, {Err: errMock}}}, 3)
	if err := insert.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("insert: %v", err)
	}

	prepare := mockStore(t, dbmock.Config{PrepareErr: errMock}, 3)
	if err := prepare.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("prepare: %v", err)
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock}, 3)
	if err := commit.Replace(ctx, embeddings); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}
}

func TestReplaceReportsASerializationFailure(t *testing.T) {
	withFailingSerializer(t)
	store := mockStore(t, dbmock.Config{}, 3)

	err := store.Replace(context.Background(), []storage.ChunkEmbedding{{ChunkID: 1, Vector: []float32{1, 2, 3}}})
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestSearchReturnsNothingForANonPositiveLimit(t *testing.T) {
	store := mockStore(t, dbmock.Config{}, 3)

	hits, err := store.Search(context.Background(), []float32{1, 2, 3}, 0)
	if err != nil || hits != nil {
		t.Fatalf("got %v %v", hits, err)
	}
}

func TestSearchReportsASerializationFailure(t *testing.T) {
	withFailingSerializer(t)
	store := mockStore(t, dbmock.Config{}, 3)

	if _, err := store.Search(context.Background(), []float32{1, 2, 3}, 5); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestSearchReportsScanAndIterationFailures(t *testing.T) {
	ctx := context.Background()
	query := []float32{1, 2, 3}

	mistyped := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"chunk_id", "distance"},
		Values:  [][]driver.Value{{"not-an-id", 0.5}},
	}}, 3)
	if _, err := mistyped.Search(ctx, query, 5); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"chunk_id", "distance"},
		Values:  [][]driver.Value{{int64(1), 0.5}},
		IterErr: errMock,
	}}, 3)
	if _, err := broken.Search(ctx, query, 5); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestOpenVectorStorageOpensTheStore(t *testing.T) {
	withOpenDB(t, dbmock.Config{}, nil)

	store, err := OpenVectorStorage(context.Background(), "x.db", 8)
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

	store, err := OpenVectorStorage(context.Background(), "x.db", 8)
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
	if store != nil {
		t.Fatal("a failure must yield a nil storage.VectorStorage, not a typed nil")
	}
}
