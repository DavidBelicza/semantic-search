package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"database/sql"
	"database/sql/driver"
	storage "github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/internal/dbmock"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run postgres integration tests")
	}

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS chunks, documents CASCADE"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}

	return store
}

func TestPostgresDocumentAndChunkLifecycle(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/notes.txt", SizeBytes: 10, ModifiedAtNS: 100},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	docs, err := store.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil {
		t.Fatalf("by status: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("want 1 indexed document, got %d", len(docs))
	}
	documentID := docs[0].ID

	inserted, err := store.ApplyDocumentChunkReconcile(ctx, documentID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hello world", TokenCount: 2, ContentHash: "h1"}},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(inserted) != 1 || inserted[0].ID == 0 {
		t.Fatalf("expected one inserted chunk with an id, got %+v", inserted)
	}

	chunks, err := store.ChunksByDocumentID(ctx, documentID)
	if err != nil || len(chunks) != 1 || chunks[0].Text != "hello world" {
		t.Fatalf("chunks by document: %v %+v", err, chunks)
	}

	meta, err := store.ChunkMetadataByIDs(ctx, []int64{inserted[0].ID})
	if err != nil || len(meta) != 1 || meta[0].Title != "Intro" {
		t.Fatalf("chunk metadata: %v %+v", err, meta)
	}

	if err := store.MarkDocumentEmbedded(ctx, "f1", "h1"); err != nil {
		t.Fatalf("mark embedded: %v", err)
	}
	embedded, err := store.DocumentsByStatus(ctx, "embedded", 0, 10)
	if err != nil || len(embedded) != 1 || embedded[0].EmbeddedContentHash != "h1" {
		t.Fatalf("embedded documents: %v %+v", err, embedded)
	}
}

func TestPostgresUpdateMissingDocumentErrors(t *testing.T) {
	store := testStore(t)
	if err := store.UpdateDocumentStatus(context.Background(), "missing", "scanned"); err == nil {
		t.Fatal("expected error updating a missing document")
	}
}

func TestPostgresDocumentsFromIDAndDelete(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/a.txt", SizeBytes: 1, ModifiedAtNS: 1},
		{FileID: "f2", AbsolutePath: "/b.txt", SizeBytes: 1, ModifiedAtNS: 1},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	all, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("documents from id: %v %+v", err, all)
	}

	target := all[0]
	if _, err := store.ApplyDocumentChunkReconcile(ctx, target.ID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hi", TokenCount: 1, ContentHash: "h1"}},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if err := store.DeleteDocument(ctx, target.ID); err != nil {
		t.Fatalf("delete document: %v", err)
	}

	remaining, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil || len(remaining) != 1 || remaining[0].ID == target.ID {
		t.Fatalf("expected the target document removed, got %v %+v", err, remaining)
	}
	chunks, err := store.ChunksByDocumentID(ctx, target.ID)
	if err != nil || len(chunks) != 0 {
		t.Fatalf("expected the document's chunks removed, got %v %+v", err, chunks)
	}
}

func TestPostgresChunkLookups(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/a.txt", SizeBytes: 1, ModifiedAtNS: 1},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	docs, err := store.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil || len(docs) != 1 {
		t.Fatalf("by status: %v %+v", err, docs)
	}
	docID := docs[0].ID

	inserted, err := store.ApplyDocumentChunkReconcile(ctx, docID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hi", ContentHash: "h1"}},
	})
	if err != nil || len(inserted) != 1 {
		t.Fatalf("reconcile: %v %+v", err, inserted)
	}
	chunkID := inserted[0].ID

	mapping, err := store.ChunkDocumentIDs(ctx, []int64{chunkID})
	if err != nil || len(mapping) != 1 || mapping[0].DocumentID != docID {
		t.Fatalf("chunk document ids: %v %+v", err, mapping)
	}
	byIDs, err := store.DocumentsByIDs(ctx, []int64{docID})
	if err != nil || len(byIDs) != 1 || byIDs[0].AbsolutePath != "/a.txt" {
		t.Fatalf("documents by ids: %v %+v", err, byIDs)
	}

	if m, err := store.ChunkDocumentIDs(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty mapping: %v %+v", err, m)
	}
	if d, err := store.DocumentsByIDs(ctx, nil); err != nil || len(d) != 0 {
		t.Fatalf("empty documents: %v %+v", err, d)
	}
}

func TestPostgresReconcileKeepAndUpdates(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/a.md", SizeBytes: 1, ModifiedAtNS: 1},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	docs, err := store.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil || len(docs) != 1 {
		t.Fatalf("by status: %v %+v", err, docs)
	}
	id := docs[0].ID

	if _, err := store.ApplyDocumentChunkReconcile(ctx, id, storage.ReconcileChunks(nil, []storage.Chunk{
		{ChunkIndex: 0, Text: "a", ContentHash: "h1"},
		{ChunkIndex: 1, Text: "b", ContentHash: "h2"},
	})); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	existing, err := store.ChunksByDocumentID(ctx, id)
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	plan := storage.ReconcileChunks(existing, []storage.Chunk{
		{ChunkIndex: 0, Text: "b", ContentHash: "h2"},
		{ChunkIndex: 1, Text: "a", ContentHash: "h1"},
		{ChunkIndex: 2, Text: "c", ContentHash: "h3"},
	})
	if _, err := store.ApplyDocumentChunkReconcile(ctx, id, plan); err != nil {
		t.Fatalf("re-reconcile: %v", err)
	}

	if err := store.UpdateDocumentContentHashAndStatus(ctx, "f1", "hash", "scanned"); err != nil {
		t.Fatalf("update content hash: %v", err)
	}
	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, "f1", "embedded"); err != nil {
		t.Fatalf("update checkpoint: %v", err)
	}
}

func TestPostgresMethodsErrorOnClosedStore(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	store.Close()

	if _, err := store.DocumentsByStatus(ctx, "indexed", 0, 10); err == nil {
		t.Fatal("expected error: DocumentsByStatus")
	}
	if _, err := store.ChunkMetadataByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: ChunkMetadataByIDs")
	}
	if _, err := store.ChunkDocumentIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: ChunkDocumentIDs")
	}
	if _, err := store.DocumentsByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: DocumentsByIDs")
	}
	if _, err := store.DocumentsFromID(ctx, 0, 10); err == nil {
		t.Fatal("expected error: DocumentsFromID")
	}
	if _, err := store.ChunksByDocumentID(ctx, 1); err == nil {
		t.Fatal("expected error: ChunksByDocumentID")
	}
	if err := store.DeleteDocument(ctx, 1); err == nil {
		t.Fatal("expected error: DeleteDocument")
	}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{{FileID: "x", AbsolutePath: "/x"}}); err == nil {
		t.Fatal("expected error: UpsertDocuments")
	}
}

func TestStoreMethodsErrorOnClosedDB(t *testing.T) {
	store := testStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	ids := []int64{1}
	files := []storage.FileMetadata{{FileID: "f", AbsolutePath: "/a"}}
	plan := storage.ChunkReconcilePlan{Insert: []storage.Chunk{{ChunkIndex: 0, Text: "x"}}}

	execs := []struct {
		name string
		err  error
	}{
		{"EnsureSchema", store.EnsureSchema(ctx)},
		{"UpsertDocuments", store.UpsertDocuments(ctx, files)},
		{"UpdateDocumentContentHashAndStatus", store.UpdateDocumentContentHashAndStatus(ctx, "f", "h", "scanned")},
		{"UpdateDocumentStatus", store.UpdateDocumentStatus(ctx, "f", "scanned")},
		{"UpdateDocumentScanCheckpointAndStatus", store.UpdateDocumentScanCheckpointAndStatus(ctx, "f", "scanned")},
		{"MarkDocumentEmbedded", store.MarkDocumentEmbedded(ctx, "f", "h")},
		{"DeleteDocument", store.DeleteDocument(ctx, 1)},
	}
	for _, c := range execs {
		if c.err == nil {
			t.Errorf("%s: expected an error on a closed database", c.name)
		}
	}

	queries := []struct {
		name string
		err  error
	}{
		{"DocumentsByStatus", secondErr(store.DocumentsByStatus(ctx, "scanned", 0, 10))},
		{"DocumentsFromID", secondErr(store.DocumentsFromID(ctx, 0, 10))},
		{"ApplyDocumentChunkReconcile", secondErr(store.ApplyDocumentChunkReconcile(ctx, 1, plan))},
		{"ChunkMetadataByIDs", secondErr(store.ChunkMetadataByIDs(ctx, ids))},
		{"ChunkDocumentIDs", secondErr(store.ChunkDocumentIDs(ctx, ids))},
		{"DocumentsByIDs", secondErr(store.DocumentsByIDs(ctx, ids))},
		{"ChunksByDocumentID", secondErr(store.ChunksByDocumentID(ctx, 1))},
	}
	for _, c := range queries {
		if c.err == nil {
			t.Errorf("%s: expected an error on a closed database", c.name)
		}
	}
}

func secondErr[T any](_ T, err error) error { return err }

var errMock = errors.New("mock failure")

var okResponse = dbmock.Response{
	Columns: []string{"id"},
	Values:  [][]driver.Value{{int64(10)}},
	Result:  dbmock.Result{Rows: 1, LastInsertID: 10},
}

func mockStore(t *testing.T, config dbmock.Config) *Store {
	t.Helper()
	db := dbmock.Open(config)
	t.Cleanup(func() { _ = db.Close() })

	return &Store{db: db}
}

func okConfig() dbmock.Config {
	return dbmock.Config{Fallback: okResponse}
}

func failAt(position int) dbmock.Config {
	responses := make([]dbmock.Response, 0, position+1)
	for i := 0; i < position; i++ {
		responses = append(responses, okResponse)
	}

	return dbmock.Config{Responses: append(responses, dbmock.Response{Err: errMock}), Fallback: okResponse}
}

func rowsConfig(columns []string, values [][]driver.Value) dbmock.Config {
	return dbmock.Config{Fallback: dbmock.Response{Columns: columns, Values: values}}
}

func testChunk() storage.Chunk {
	return storage.Chunk{
		ID: 1, DocumentID: 2, ChunkIndex: 0, Title: "t", Text: "x",
		TokenCount: 1, StartOffset: 0, EndOffset: 1, ContentHash: "h",
	}
}

func TestOpenReportsAConnectionFailure(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) { return nil, errMock }
	defer func() { openDB = original }()

	if _, err := Open(""); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAndCloseBuildAndReleaseTheStore(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) { return dbmock.Open(okConfig()), nil }
	defer func() { openDB = original }()

	store, err := Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEnsureSchemaRunsAndReportsFailure(t *testing.T) {
	ctx := context.Background()

	if err := mockStore(t, okConfig()).EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := mockStore(t, failAt(0)).EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestUpsertDocumentsRunsAndReportsEachFailure(t *testing.T) {
	ctx := context.Background()
	files := []storage.FileMetadata{{FileID: "a", AbsolutePath: "/a", SizeBytes: 1, ModifiedAtNS: 2}}

	if err := mockStore(t, okConfig()).UpsertDocuments(ctx, files); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock, Fallback: okResponse})
	if err := begin.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}

	prepare := mockStore(t, dbmock.Config{PrepareErr: errMock, Fallback: okResponse})
	if err := prepare.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("prepare: %v", err)
	}

	if err := mockStore(t, failAt(0)).UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("exec: %v", err)
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock, Fallback: okResponse})
	if err := commit.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}
}

func TestDocumentsFromIDReadsRowsAndReportsFailures(t *testing.T) {
	ctx := context.Background()

	store := mockStore(t, rowsConfig(
		[]string{"id", "absolute_path"},
		[][]driver.Value{{int64(1), "/a"}, {int64(2), "/b"}},
	))
	documents, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil {
		t.Fatalf("documents: %v", err)
	}
	if len(documents) != 2 || documents[1].AbsolutePath != "/b" {
		t.Fatalf("got %+v", documents)
	}

	if _, err := mockStore(t, failAt(0)).DocumentsFromID(ctx, 0, 10); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, rowsConfig([]string{"id", "absolute_path"}, [][]driver.Value{{"x", "/a"}}))
	if _, err := mistyped.DocumentsFromID(ctx, 0, 10); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"id", "absolute_path"},
		Values:  [][]driver.Value{{int64(1), "/a"}},
		IterErr: errMock,
	}})
	if _, err := broken.DocumentsFromID(ctx, 0, 10); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestDocumentsByStatusReadsNullableColumns(t *testing.T) {
	ctx := context.Background()
	columns := []string{
		"id", "file_id", "absolute_path", "file_size", "modified_at_ns",
		"content_hash", "scanned_file_size", "scanned_modified_at_ns", "status", "embedded_content_hash",
	}

	filled := mockStore(t, rowsConfig(columns, [][]driver.Value{
		{int64(1), "f", "/a", int64(10), int64(20), "hash", int64(10), int64(20), "indexed", "embedded-hash"},
	}))
	documents, err := filled.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil {
		t.Fatalf("documents: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("got %d documents", len(documents))
	}
	if !documents[0].HasHash || !documents[0].HasScannedMetadata {
		t.Fatalf("nullable columns not read: %+v", documents[0])
	}
	if documents[0].ContentHash != "hash" || documents[0].EmbeddedContentHash != "embedded-hash" {
		t.Fatalf("got %+v", documents[0])
	}

	empty := mockStore(t, rowsConfig(columns, [][]driver.Value{
		{int64(1), "f", "/a", int64(10), int64(20), nil, nil, nil, "indexed", nil},
	}))
	documents, err = empty.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil {
		t.Fatalf("documents: %v", err)
	}
	if documents[0].HasHash || documents[0].HasScannedMetadata {
		t.Fatalf("null columns should not be marked present: %+v", documents[0])
	}
}

func TestDocumentsByStatusReportsFailures(t *testing.T) {
	ctx := context.Background()
	columns := []string{
		"id", "file_id", "absolute_path", "file_size", "modified_at_ns",
		"content_hash", "scanned_file_size", "scanned_modified_at_ns", "status", "embedded_content_hash",
	}

	if _, err := mockStore(t, failAt(0)).DocumentsByStatus(ctx, "indexed", 0, 10); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, rowsConfig(columns, [][]driver.Value{
		{"not-an-id", "f", "/a", int64(10), int64(20), nil, nil, nil, "indexed", nil},
	}))
	if _, err := mistyped.DocumentsByStatus(ctx, "indexed", 0, 10); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: columns,
		Values: [][]driver.Value{
			{int64(1), "f", "/a", int64(10), int64(20), nil, nil, nil, "indexed", nil},
		},
		IterErr: errMock,
	}})
	if _, err := broken.DocumentsByStatus(ctx, "indexed", 0, 10); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestChunkMetadataByIDsReadsRowsAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	columns := []string{"id", "document_id", "title", "text"}

	if metadata, err := mockStore(t, okConfig()).ChunkMetadataByIDs(ctx, nil); err != nil || metadata != nil {
		t.Fatalf("empty id list should return nothing: %v %v", metadata, err)
	}

	store := mockStore(t, rowsConfig(columns, [][]driver.Value{{int64(1), int64(2), "t", "x"}}))
	metadata, err := store.ChunkMetadataByIDs(ctx, []int64{1})
	if err != nil || len(metadata) != 1 || metadata[0].Title != "t" {
		t.Fatalf("got %+v %v", metadata, err)
	}

	if _, err := mockStore(t, failAt(0)).ChunkMetadataByIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, rowsConfig(columns, [][]driver.Value{{"x", int64(2), "t", "x"}}))
	if _, err := mistyped.ChunkMetadataByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: columns, Values: [][]driver.Value{{int64(1), int64(2), "t", "x"}}, IterErr: errMock,
	}})
	if _, err := broken.ChunkMetadataByIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestChunkDocumentIDsReadsRowsAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	columns := []string{"id", "document_id"}

	if mapping, err := mockStore(t, okConfig()).ChunkDocumentIDs(ctx, nil); err != nil || mapping != nil {
		t.Fatalf("empty id list should return nothing: %v %v", mapping, err)
	}

	store := mockStore(t, rowsConfig(columns, [][]driver.Value{{int64(1), int64(2)}}))
	mapping, err := store.ChunkDocumentIDs(ctx, []int64{1})
	if err != nil || len(mapping) != 1 || mapping[0].DocumentID != 2 {
		t.Fatalf("got %+v %v", mapping, err)
	}

	if _, err := mockStore(t, failAt(0)).ChunkDocumentIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, rowsConfig(columns, [][]driver.Value{{"x", int64(2)}}))
	if _, err := mistyped.ChunkDocumentIDs(ctx, []int64{1}); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: columns, Values: [][]driver.Value{{int64(1), int64(2)}}, IterErr: errMock,
	}})
	if _, err := broken.ChunkDocumentIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestDocumentsByIDsReadsRowsAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	columns := []string{"id", "absolute_path"}

	if documents, err := mockStore(t, okConfig()).DocumentsByIDs(ctx, nil); err != nil || documents != nil {
		t.Fatalf("empty id list should return nothing: %v %v", documents, err)
	}

	store := mockStore(t, rowsConfig(columns, [][]driver.Value{{int64(1), "/a"}}))
	documents, err := store.DocumentsByIDs(ctx, []int64{1})
	if err != nil || len(documents) != 1 || documents[0].AbsolutePath != "/a" {
		t.Fatalf("got %+v %v", documents, err)
	}

	if _, err := mockStore(t, failAt(0)).DocumentsByIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, rowsConfig(columns, [][]driver.Value{{"x", "/a"}}))
	if _, err := mistyped.DocumentsByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: columns, Values: [][]driver.Value{{int64(1), "/a"}}, IterErr: errMock,
	}})
	if _, err := broken.DocumentsByIDs(ctx, []int64{1}); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestChunksByDocumentIDReadsRowsAndReportsFailures(t *testing.T) {
	ctx := context.Background()
	columns := []string{
		"id", "document_id", "chunk_index", "title", "text",
		"token_count", "start_offset", "end_offset", "content_hash",
	}
	row := []driver.Value{int64(1), int64(2), int64(0), "t", "x", int64(1), int64(0), int64(1), "h"}

	store := mockStore(t, rowsConfig(columns, [][]driver.Value{row}))
	chunks, err := store.ChunksByDocumentID(ctx, 2)
	if err != nil || len(chunks) != 1 || chunks[0].Title != "t" {
		t.Fatalf("got %+v %v", chunks, err)
	}

	if _, err := mockStore(t, failAt(0)).ChunksByDocumentID(ctx, 2); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	bad := append([]driver.Value{"not-an-id"}, row[1:]...)
	mistyped := mockStore(t, rowsConfig(columns, [][]driver.Value{bad}))
	if _, err := mistyped.ChunksByDocumentID(ctx, 2); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: columns, Values: [][]driver.Value{row}, IterErr: errMock,
	}})
	if _, err := broken.ChunksByDocumentID(ctx, 2); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestDeleteDocumentRunsAndReportsEachFailure(t *testing.T) {
	ctx := context.Background()

	if err := mockStore(t, okConfig()).DeleteDocument(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock, Fallback: okResponse})
	if err := begin.DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}
	if err := mockStore(t, failAt(0)).DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("delete chunks: %v", err)
	}
	if err := mockStore(t, failAt(1)).DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("delete document: %v", err)
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock, Fallback: okResponse})
	if err := commit.DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}
}

func TestDocumentUpdatesRunThroughEveryCaller(t *testing.T) {
	ctx := context.Background()
	store := mockStore(t, okConfig())

	if err := store.UpdateDocumentContentHashAndStatus(ctx, "f", "hash", "scanned"); err != nil {
		t.Fatalf("content hash: %v", err)
	}
	if err := store.UpdateDocumentStatus(ctx, "f", "indexed"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := store.MarkDocumentEmbedded(ctx, "f", "hash"); err != nil {
		t.Fatalf("embedded: %v", err)
	}
	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, "f", "scanned"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}

func TestUpdateDocumentReportsFailuresAndAMissingRow(t *testing.T) {
	ctx := context.Background()

	if err := mockStore(t, failAt(0)).UpdateDocumentStatus(ctx, "f", "indexed"); !errors.Is(err, errMock) {
		t.Fatalf("exec: %v", err)
	}

	reportFailure := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Result: dbmock.Result{RowsErr: errMock},
	}})
	if err := reportFailure.UpdateDocumentStatus(ctx, "f", "indexed"); !errors.Is(err, errMock) {
		t.Fatalf("rows affected: %v", err)
	}

	missing := mockStore(t, dbmock.Config{Fallback: dbmock.Response{Result: dbmock.Result{Rows: 0}}})
	if err := missing.UpdateDocumentStatus(ctx, "f", "indexed"); err == nil {
		t.Fatal("want an error when no document matched")
	}
}

func TestApplyDocumentChunkReconcileRunsTheWholePlan(t *testing.T) {
	store := mockStore(t, okConfig())

	inserted, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{
		RemoveIDs: []int64{9},
		Keep:      []storage.Chunk{testChunk()},
		Insert:    []storage.Chunk{testChunk()},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(inserted) != 1 || inserted[0].ID != 10 || inserted[0].DocumentID != 2 {
		t.Fatalf("got %+v", inserted)
	}
}

func TestApplyDocumentChunkReconcileSkipsEmptyKeepAndRemove(t *testing.T) {
	store := mockStore(t, okConfig())

	inserted, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(inserted) != 0 {
		t.Fatalf("got %+v", inserted)
	}
}

func TestApplyDocumentChunkReconcileReportsEachStepFailure(t *testing.T) {
	ctx := context.Background()
	plan := storage.ChunkReconcilePlan{
		RemoveIDs: []int64{9},
		Keep:      []storage.Chunk{testChunk()},
		Insert:    []storage.Chunk{testChunk()},
	}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock, Fallback: okResponse})
	if _, err := begin.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}

	for position, name := range []string{"delete", "park", "update", "insert"} {
		store := mockStore(t, failAt(position))
		if _, err := store.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock, Fallback: okResponse})
	if _, err := commit.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}

	for position, name := range []string{"park", "update", "insert"} {
		errs := make([]error, position+1)
		errs[position] = errMock
		store := mockStore(t, dbmock.Config{PrepareErrs: errs, Fallback: okResponse})
		if _, err := store.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
			t.Fatalf("%s prepare: %v", name, err)
		}
	}
}

func TestApplyDocumentChunkReconcileReportsAnUnreadableInsertedID(t *testing.T) {
	store := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"id"},
		Values:  [][]driver.Value{{"not-an-id"}},
	}})

	_, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{testChunk()},
	})
	if err == nil {
		t.Fatal("want a scan failure")
	}
}

func TestKeptChunkHelpersReportPrepareFailures(t *testing.T) {
	store := mockStore(t, dbmock.Config{PrepareErr: errMock, Fallback: okResponse})

	_, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{
		Keep: []storage.Chunk{testChunk()},
	})
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestInQueryNumbersItsPlaceholders(t *testing.T) {
	query, args := inQuery("DELETE FROM chunks WHERE id IN (", []int64{7, 8})

	if query != "DELETE FROM chunks WHERE id IN ($1, $2)" {
		t.Fatalf("got %q", query)
	}
	if len(args) != 2 || args[0] != int64(7) || args[1] != int64(8) {
		t.Fatalf("got %v", args)
	}
}

func TestOpenStorageOpensAndPreparesTheSchema(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) { return dbmock.Open(okConfig()), nil }
	defer func() { openDB = original }()

	store, err := OpenStorage(context.Background(), "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("want a store")
	}
}

func TestOpenStorageReportsAnOpenFailure(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) { return nil, errMock }
	defer func() { openDB = original }()

	store, err := OpenStorage(context.Background(), "")
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
	if store != nil {
		t.Fatal("a failure must yield a nil storage.Storage, not a typed nil")
	}
}

func TestOpenStorageReportsASchemaFailureAndReleasesTheHandle(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) {
		return dbmock.Open(dbmock.Config{Fallback: dbmock.Response{Err: errMock}}), nil
	}
	defer func() { openDB = original }()

	store, err := OpenStorage(context.Background(), "")
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
	if store != nil {
		t.Fatal("want a nil storage.Storage")
	}
}
