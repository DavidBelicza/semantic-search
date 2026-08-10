package sqlite

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

func TestEnsureSchemaCreatesDocumentsTable(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var tableName string
	err := store.db.QueryRowContext(
		context.Background(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'documents'",
	).Scan(&tableName)
	if err != nil {
		t.Fatalf("query documents table: %v", err)
	}

	if tableName != "documents" {
		t.Fatalf("table mismatch: want documents, got %q", tableName)
	}

	for _, wantTable := range []string{"chunks"} {
		err := store.db.QueryRowContext(
			context.Background(),
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
			wantTable,
		).Scan(&tableName)
		if err != nil {
			t.Fatalf("query %s table: %v", wantTable, err)
		}
		if tableName != wantTable {
			t.Fatalf("table mismatch: want %s, got %q", wantTable, tableName)
		}
	}
}

func TestEnsureSchemaMigratesOldDocumentStatuses(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `
CREATE TABLE documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	modified_at_ns INTEGER NOT NULL,
	content_hash TEXT,
	scanned_file_size INTEGER,
	scanned_modified_at_ns INTEGER,
	status TEXT NOT NULL DEFAULT 'indexed' CHECK(status IN ('indexed', 'scanned', 'done', 'failed')),
	indexed_at_unix INTEGER,
	deleted_at_unix INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(file_id)
)`); err != nil {
		t.Fatalf("create old documents table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO documents (file_id, absolute_path, file_size, modified_at_ns, content_hash, scanned_file_size, scanned_modified_at_ns, status)
VALUES ('1:100', '/tmp/docs/README.md', 10, 100, 'hash', 10, 100, 'done')`); err != nil {
		t.Fatalf("insert old document: %v", err)
	}

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var status string
	if err := store.db.QueryRowContext(ctx, "SELECT status FROM documents WHERE file_id = '1:100'").Scan(&status); err != nil {
		t.Fatalf("query migrated status: %v", err)
	}
	if status != storage.DocumentStatusIndexed {
		t.Fatalf("status mismatch: want indexed, got %q", status)
	}

	if _, err := store.db.ExecContext(ctx, "UPDATE documents SET status = ? WHERE file_id = '1:100'", storage.DocumentStatusEmbedded); err != nil {
		t.Fatalf("new embedded status should satisfy migrated constraint: %v", err)
	}
}

func TestEnsureSchemaAddsEmbeddedContentHashColumnToLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `
CREATE TABLE documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id TEXT NOT NULL,
	absolute_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	modified_at_ns INTEGER NOT NULL,
	content_hash TEXT,
	scanned_file_size INTEGER,
	scanned_modified_at_ns INTEGER,
	status TEXT NOT NULL DEFAULT 'indexed' CHECK(status IN ('indexed', 'scanned', 'chunked', 'embedded')),
	indexed_at_unix INTEGER,
	deleted_at_unix INTEGER,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(file_id)
)`); err != nil {
		t.Fatalf("create legacy documents table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO documents (file_id, absolute_path, file_size, modified_at_ns, content_hash, scanned_file_size, scanned_modified_at_ns, status)
VALUES ('1:100', '/tmp/docs/README.md', 10, 100, 'hash', 10, 100, 'scanned')`); err != nil {
		t.Fatalf("insert legacy document: %v", err)
	}

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	exists, err := store.tableColumnExists(ctx, "documents", "embedded_content_hash")
	if err != nil {
		t.Fatalf("check column: %v", err)
	}
	if !exists {
		t.Fatal("expected embedded_content_hash column to be added to legacy database")
	}

	chunkTitleExists, err := store.tableColumnExists(ctx, "chunks", "title")
	if err != nil {
		t.Fatalf("check chunk title column: %v", err)
	}
	if !chunkTitleExists {
		t.Fatal("expected chunks.title column to be present")
	}

	if err := store.MarkDocumentEmbedded(ctx, "1:100", "content-hash"); err != nil {
		t.Fatalf("mark embedded: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusEmbedded, 0, 10)
	if err != nil {
		t.Fatalf("documents by status: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("document count mismatch: want 1, got %d", len(documents))
	}
	if documents[0].EmbeddedContentHash != "content-hash" {
		t.Fatalf("embedded content hash mismatch: got %q", documents[0].EmbeddedContentHash)
	}
}

func TestApplyDocumentChunkReconcileSwapsKeptChunkIndexesWithoutUniqueViolation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/note.md"),
		SizeBytes:    2,
		ModifiedAtNS: 1,
	}}); err != nil {
		t.Fatalf("upsert document: %v", err)
	}

	var documentID int64
	if err := store.db.QueryRowContext(ctx, "SELECT id FROM documents WHERE file_id = '1:100'").Scan(&documentID); err != nil {
		t.Fatalf("load document id: %v", err)
	}

	initial := storage.ChunkReconcilePlan{Insert: []storage.Chunk{
		{ChunkIndex: 0, Text: "A", TokenCount: 1, StartOffset: 0, EndOffset: 1, ContentHash: "hash-a"},
		{ChunkIndex: 1, Text: "B", TokenCount: 1, StartOffset: 1, EndOffset: 2, ContentHash: "hash-b"},
	}}
	if _, err := store.ApplyDocumentChunkReconcile(ctx, documentID, initial); err != nil {
		t.Fatalf("initial insert: %v", err)
	}

	existing, err := store.ChunksByDocumentID(ctx, documentID)
	if err != nil {
		t.Fatalf("load chunks: %v", err)
	}

	incoming := []storage.Chunk{
		{ChunkIndex: 0, Text: "B", TokenCount: 1, StartOffset: 0, EndOffset: 1, ContentHash: "hash-b"},
		{ChunkIndex: 1, Text: "A", TokenCount: 1, StartOffset: 1, EndOffset: 2, ContentHash: "hash-a"},
	}
	plan := storage.ReconcileChunks(existing, incoming)
	if len(plan.Keep) != 2 || len(plan.Insert) != 0 || len(plan.RemoveIDs) != 0 {
		t.Fatalf("expected a pure reorder plan, got %#v", plan)
	}

	if _, err := store.ApplyDocumentChunkReconcile(ctx, documentID, plan); err != nil {
		t.Fatalf("swap reconcile: %v", err)
	}

	final, err := store.ChunksByDocumentID(ctx, documentID)
	if err != nil {
		t.Fatalf("load swapped chunks: %v", err)
	}
	if len(final) != 2 {
		t.Fatalf("chunk count mismatch: want 2, got %d", len(final))
	}
	if final[0].ContentHash != "hash-b" || final[1].ContentHash != "hash-a" {
		t.Fatalf("chunk indexes were not swapped: %#v", final)
	}
}

func TestUpsertDocumentsInsertsAndUpdatesInBatch(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	first := storage.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	second := storage.FileMetadata{
		FileID:       "1:200",
		AbsolutePath: filepath.Clean("/tmp/docs/notes/plan.md"),
		SizeBytes:    20,
		ModifiedAtNS: 200,
	}

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{first, second}); err != nil {
		t.Fatalf("insert documents: %v", err)
	}

	if err := store.UpdateDocumentStatus(ctx, second.FileID, storage.DocumentStatusEmbedded); err != nil {
		t.Fatalf("mark second embedded: %v", err)
	}

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{first, second}); err != nil {
		t.Fatalf("upsert unchanged documents: %v", err)
	}

	first.SizeBytes = 15
	first.ModifiedAtNS = 150
	first.AbsolutePath = filepath.Clean("/tmp/docs/README-renamed.md")
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{first}); err != nil {
		t.Fatalf("update document: %v", err)
	}

	rows, err := store.db.QueryContext(ctx, `
SELECT file_id, absolute_path, file_size, modified_at_ns, status
FROM documents
ORDER BY file_id`)
	if err != nil {
		t.Fatalf("query documents: %v", err)
	}
	defer rows.Close()

	got := map[string]struct {
		absolutePath string
		size         int64
		modifiedNS   int64
		status       string
	}{}
	for rows.Next() {
		var fileID string
		var absolutePath string
		var size int64
		var modifiedNS int64
		var status string
		if err := rows.Scan(&fileID, &absolutePath, &size, &modifiedNS, &status); err != nil {
			t.Fatalf("scan document: %v", err)
		}
		got[fileID] = struct {
			absolutePath string
			size         int64
			modifiedNS   int64
			status       string
		}{absolutePath: absolutePath, size: size, modifiedNS: modifiedNS, status: status}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate documents: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("document count mismatch: want 2, got %d", len(got))
	}
	if got["1:100"].absolutePath != first.AbsolutePath || got["1:100"].size != 15 || got["1:100"].modifiedNS != 150 {
		t.Fatalf("first document was not updated: %#v", got["1:100"])
	}
	if got["1:100"].status != storage.DocumentStatusIndexed {
		t.Fatalf("first document status mismatch: want indexed, got %q", got["1:100"].status)
	}
	if got["1:200"].size != 20 {
		t.Fatalf("second document changed unexpectedly: %#v", got["1:200"])
	}
	if got["1:200"].status != storage.DocumentStatusEmbedded {
		t.Fatalf("second document status mismatch: want embedded, got %q", got["1:200"].status)
	}
}

func TestDocumentsByStatusAndScanUpdates(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	file := storage.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{file}); err != nil {
		t.Fatalf("insert document: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusIndexed, 0, 10)
	if err != nil {
		t.Fatalf("documents by status: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("document count mismatch: want 1, got %d", len(documents))
	}
	if documents[0].HasHash {
		t.Fatalf("expected missing content hash, got %q", documents[0].ContentHash)
	}

	const wantHash = "abc123"
	if err := store.UpdateDocumentContentHashAndStatus(ctx, file.FileID, wantHash, storage.DocumentStatusScanned); err != nil {
		t.Fatalf("update content hash and status: %v", err)
	}

	documents, err = store.DocumentsByStatus(ctx, storage.DocumentStatusScanned, 0, 10)
	if err != nil {
		t.Fatalf("documents by scanned status: %v", err)
	}
	if len(documents) != 1 || !documents[0].HasHash || documents[0].ContentHash != wantHash {
		t.Fatalf("scanned document mismatch: %#v", documents)
	}

	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, file.FileID, storage.DocumentStatusEmbedded); err != nil {
		t.Fatalf("update scan checkpoint and status: %v", err)
	}

	documents, err = store.DocumentsByStatus(ctx, storage.DocumentStatusEmbedded, 0, 10)
	if err != nil {
		t.Fatalf("documents by embedded status: %v", err)
	}
	if len(documents) != 1 || !documents[0].HasScannedMetadata {
		t.Fatalf("embedded document mismatch: %#v", documents)
	}
}

func TestApplyDocumentChunkReconcileKeepsInsertsAndDeletes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	file := storage.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{file}); err != nil {
		t.Fatalf("insert document: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusIndexed, 0, 1)
	if err != nil {
		t.Fatalf("documents by status: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("document count mismatch: want 1, got %d", len(documents))
	}

	chunks := []storage.Chunk{
		{ChunkIndex: 0, Text: "hello", TokenCount: 2, StartOffset: 0, EndOffset: 5, ContentHash: "hash-1"},
		{ChunkIndex: 1, Text: "world", TokenCount: 2, StartOffset: 5, EndOffset: 10, ContentHash: "hash-2"},
	}
	initialPlan := storage.ReconcileChunks(nil, chunks)
	inserted, err := store.ApplyDocumentChunkReconcile(ctx, documents[0].ID, initialPlan)
	if err != nil {
		t.Fatalf("apply initial chunks: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("inserted chunk count mismatch: want 2, got %d", len(inserted))
	}

	var chunkCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks WHERE document_id = ?", documents[0].ID).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 2 {
		t.Fatalf("chunk count mismatch: want 2, got %d", chunkCount)
	}

	existing, err := store.ChunksByDocumentID(ctx, documents[0].ID)
	if err != nil {
		t.Fatalf("load existing chunks: %v", err)
	}

	nextChunks := []storage.Chunk{
		{ChunkIndex: 0, Text: "hello", TokenCount: 2, StartOffset: 0, EndOffset: 5, ContentHash: "hash-1"},
		{ChunkIndex: 1, Text: "new", TokenCount: 1, StartOffset: 5, EndOffset: 8, ContentHash: "hash-3"},
	}
	plan := storage.ReconcileChunks(existing, nextChunks)
	if len(plan.Keep) != 1 || plan.Keep[0].ID != existing[0].ID {
		t.Fatalf("kept chunks mismatch: %#v", plan.Keep)
	}
	if len(plan.Insert) != 1 || plan.Insert[0].ContentHash != "hash-3" {
		t.Fatalf("insert chunks mismatch: %#v", plan.Insert)
	}
	if len(plan.RemoveIDs) != 1 || plan.RemoveIDs[0] != existing[1].ID {
		t.Fatalf("removed chunks mismatch: %#v", plan.RemoveIDs)
	}

	inserted, err = store.ApplyDocumentChunkReconcile(ctx, documents[0].ID, plan)
	if err != nil {
		t.Fatalf("apply reconciled chunks: %v", err)
	}
	if len(inserted) != 1 || inserted[0].ID == 0 || inserted[0].ContentHash != "hash-3" {
		t.Fatalf("inserted reconciled chunks mismatch: %#v", inserted)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks WHERE document_id = ?", documents[0].ID).Scan(&chunkCount); err != nil {
		t.Fatalf("count reconciled chunks: %v", err)
	}
	if chunkCount != 2 {
		t.Fatalf("reconciled chunk count mismatch: want 2, got %d", chunkCount)
	}
}

func TestReconcileChunksHandlesDuplicateContentHashesByOccurrence(t *testing.T) {
	existing := []storage.Chunk{
		{ID: 10, DocumentID: 42, ChunkIndex: 0, ContentHash: "same"},
		{ID: 11, DocumentID: 42, ChunkIndex: 1, ContentHash: "same"},
		{ID: 12, DocumentID: 42, ChunkIndex: 2, ContentHash: "removed"},
	}
	incoming := []storage.Chunk{
		{ChunkIndex: 0, ContentHash: "same"},
		{ChunkIndex: 1, ContentHash: "same"},
		{ChunkIndex: 2, ContentHash: "new"},
	}

	plan := storage.ReconcileChunks(existing, incoming)

	if len(plan.Keep) != 2 || plan.Keep[0].ID != 10 || plan.Keep[1].ID != 11 {
		t.Fatalf("kept duplicate chunks mismatch: %#v", plan.Keep)
	}
	if len(plan.Insert) != 1 || plan.Insert[0].ContentHash != "new" {
		t.Fatalf("insert chunks mismatch: %#v", plan.Insert)
	}
	if len(plan.RemoveIDs) != 1 || plan.RemoveIDs[0] != 12 {
		t.Fatalf("removed chunks mismatch: %#v", plan.RemoveIDs)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}

	return store
}

func TestSearchAndCleanupQueries(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	file := storage.FileMetadata{FileID: "1:1", AbsolutePath: "/tmp/a.md", SizeBytes: 1, ModifiedAtNS: 1}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{file}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	docs, err := store.DocumentsByStatus(ctx, storage.DocumentStatusIndexed, 0, 1)
	if err != nil || len(docs) != 1 {
		t.Fatalf("by status: %v %+v", err, docs)
	}
	docID := docs[0].ID

	inserted, err := store.ApplyDocumentChunkReconcile(ctx, docID, storage.ReconcileChunks(nil, []storage.Chunk{
		{ChunkIndex: 0, Title: "Intro", Text: "hello", ContentHash: "h1"},
		{ChunkIndex: 1, Title: "Body", Text: "world", ContentHash: "h2"},
	}))
	if err != nil || len(inserted) != 2 {
		t.Fatalf("reconcile: %v %+v", err, inserted)
	}
	ids := []int64{inserted[0].ID, inserted[1].ID}

	meta, err := store.ChunkMetadataByIDs(ctx, ids)
	if err != nil || len(meta) != 2 {
		t.Fatalf("chunk metadata: %v %+v", err, meta)
	}
	mapping, err := store.ChunkDocumentIDs(ctx, ids)
	if err != nil || len(mapping) != 2 || mapping[0].DocumentID != docID {
		t.Fatalf("chunk document ids: %v %+v", err, mapping)
	}
	byIDs, err := store.DocumentsByIDs(ctx, []int64{docID})
	if err != nil || len(byIDs) != 1 || byIDs[0].AbsolutePath == "" {
		t.Fatalf("documents by ids: %v %+v", err, byIDs)
	}
	all, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("documents from id: %v %+v", err, all)
	}

	if m, err := store.ChunkMetadataByIDs(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty metadata: %v %+v", err, m)
	}
	if m, err := store.ChunkDocumentIDs(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty mapping: %v %+v", err, m)
	}
	if d, err := store.DocumentsByIDs(ctx, nil); err != nil || len(d) != 0 {
		t.Fatalf("empty documents: %v %+v", err, d)
	}

	if err := store.DeleteDocument(ctx, docID); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if remaining, err := store.DocumentsFromID(ctx, 0, 10); err != nil || len(remaining) != 0 {
		t.Fatalf("expected no documents after delete: %v %+v", err, remaining)
	}
	if left, err := store.ChunksByDocumentID(ctx, docID); err != nil || len(left) != 0 {
		t.Fatalf("expected chunks removed: %v %+v", err, left)
	}
}

func TestSqliteMethodsErrorOnClosedStore(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if store.DB() == nil {
		t.Fatal("expected a non-nil DB handle")
	}
	store.Close()

	if _, err := store.ChunkMetadataByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: ChunkMetadataByIDs on closed store")
	}
	if _, err := store.ChunkDocumentIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: ChunkDocumentIDs on closed store")
	}
	if _, err := store.DocumentsByIDs(ctx, []int64{1}); err == nil {
		t.Fatal("expected error: DocumentsByIDs on closed store")
	}
	if _, err := store.DocumentsFromID(ctx, 0, 10); err == nil {
		t.Fatal("expected error: DocumentsFromID on closed store")
	}
	if _, err := store.ChunksByDocumentID(ctx, 1); err == nil {
		t.Fatal("expected error: ChunksByDocumentID on closed store")
	}
	if err := store.DeleteDocument(ctx, 1); err == nil {
		t.Fatal("expected error: DeleteDocument on closed store")
	}
	if _, err := store.DocumentsByStatus(ctx, "indexed", 0, 10); err == nil {
		t.Fatal("expected error: DocumentsByStatus on closed store")
	}
	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{{FileID: "x", AbsolutePath: "/x"}}); err == nil {
		t.Fatal("expected error: UpsertDocuments on closed store")
	}
}

func TestStoreMethodsErrorOnClosedDB(t *testing.T) {
	store := openTestStore(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	ids := []int64{1}
	files := []storage.FileMetadata{{FileID: "f", AbsolutePath: "/a"}}
	plan := storage.ChunkReconcilePlan{Insert: []storage.Chunk{{ChunkIndex: 0, Text: "x"}}}

	checks := []struct {
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
	for _, c := range checks {
		if c.err == nil {
			t.Errorf("%s: expected an error on a closed database", c.name)
		}
	}

	queries := []struct {
		name string
		err  error
	}{
		{"DocumentsByStatus", second(store.DocumentsByStatus(ctx, "scanned", 0, 10))},
		{"DocumentsFromID", second(store.DocumentsFromID(ctx, 0, 10))},
		{"ApplyDocumentChunkReconcile", second(store.ApplyDocumentChunkReconcile(ctx, 1, plan))},
		{"ChunkMetadataByIDs", second(store.ChunkMetadataByIDs(ctx, ids))},
		{"ChunkDocumentIDs", second(store.ChunkDocumentIDs(ctx, ids))},
		{"DocumentsByIDs", second(store.DocumentsByIDs(ctx, ids))},
		{"ChunksByDocumentID", second(store.ChunksByDocumentID(ctx, 1))},
	}
	for _, c := range queries {
		if c.err == nil {
			t.Errorf("%s: expected an error on a closed database", c.name)
		}
	}
}

func second[T any](_ T, err error) error { return err }

var errMock = errors.New("mock failure")

func mockStore(t *testing.T, config dbmock.Config) *Store {
	t.Helper()
	db := dbmock.Open(config)
	t.Cleanup(func() { _ = db.Close() })

	return &Store{db: db}
}

func schemaRow(createSQL string) dbmock.Response {
	return dbmock.Response{Columns: []string{"sql"}, Values: [][]driver.Value{{createSQL}}}
}

func columnRow(name string) dbmock.Response {
	return dbmock.Response{
		Columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
		Values:  [][]driver.Value{{int64(0), name, "TEXT", int64(0), nil, int64(0)}},
	}
}

func ok() dbmock.Response { return dbmock.Response{Result: dbmock.Result{Rows: 1, LastInsertID: 5}} }

func fail() dbmock.Response { return dbmock.Response{Err: errMock} }

const modernSchema = "CREATE TABLE documents (status TEXT CHECK(status IN ('indexed')))"
const legacySchema = "CREATE TABLE documents (status TEXT CHECK(status IN ('done','failed')))"

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

	if _, err := Open("x.db"); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenReportsAFailingForeignKeyPragma(t *testing.T) {
	original := openDB
	openDB = func(string, string) (*sql.DB, error) {
		return dbmock.Open(dbmock.Config{Fallback: fail()}), nil
	}
	defer func() { openDB = original }()

	if _, err := Open("x.db"); !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureSchemaRunsWhenNoMigrationIsNeeded(t *testing.T) {
	store := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(),
		schemaRow(modernSchema),
		columnRow("embedded_content_hash"),
		columnRow("title"),
	}})

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
}

func TestEnsureSchemaAddsMissingColumns(t *testing.T) {
	store := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(),
		schemaRow(modernSchema),
		columnRow("other"),
		ok(),
		columnRow("other"),
		ok(),
	}})

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
}

func TestEnsureSchemaReportsEachStepFailure(t *testing.T) {
	ctx := context.Background()

	schema := mockStore(t, dbmock.Config{Responses: []dbmock.Response{fail()}})
	if err := schema.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("schema: %v", err)
	}

	status := mockStore(t, dbmock.Config{Responses: []dbmock.Response{ok(), fail()}})
	if err := status.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("status probe: %v", err)
	}

	embedded := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(), schemaRow(modernSchema), columnRow("other"), fail(),
	}})
	if err := embedded.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("embedded column: %v", err)
	}

	title := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(), schemaRow(modernSchema), columnRow("embedded_content_hash"), columnRow("other"), fail(),
	}})
	if err := title.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("title column: %v", err)
	}

	embeddedProbe := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(), schemaRow(modernSchema), fail(),
	}})
	if err := embeddedProbe.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("embedded column probe: %v", err)
	}

	titleProbe := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(), schemaRow(modernSchema), columnRow("embedded_content_hash"), fail(),
	}})
	if err := titleProbe.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("title column probe: %v", err)
	}
}

func TestTableColumnExistsReportsQueryScanAndIterationFailures(t *testing.T) {
	ctx := context.Background()

	failed := mockStore(t, dbmock.Config{Fallback: fail()})
	if _, err := failed.tableColumnExists(ctx, "documents", "title"); !errors.Is(err, errMock) {
		t.Fatalf("query: %v", err)
	}

	mistyped := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
		Values:  [][]driver.Value{{"not-an-int", "title", "TEXT", int64(0), nil, int64(0)}},
	}})
	if _, err := mistyped.tableColumnExists(ctx, "chunks", "title"); err == nil {
		t.Fatal("want a scan failure")
	}

	broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"},
		Values:  [][]driver.Value{{int64(0), "other", "TEXT", int64(0), nil, int64(0)}},
		IterErr: errMock,
	}})
	if _, err := broken.tableColumnExists(ctx, "chunks", "title"); !errors.Is(err, errMock) {
		t.Fatalf("iteration: %v", err)
	}
}

func TestDocumentStatusMigrationRunsAndReportsEachFailure(t *testing.T) {
	ctx := context.Background()

	full := mockStore(t, dbmock.Config{Responses: []dbmock.Response{
		ok(), schemaRow(legacySchema), ok(), ok(), ok(), ok(), ok(), ok(),
		columnRow("embedded_content_hash"), columnRow("title"),
	}})
	if err := full.EnsureSchema(ctx); err != nil {
		t.Fatalf("migration: %v", err)
	}

	for position, name := range []string{"pragma off", "create", "insert", "drop", "rename"} {
		responses := []dbmock.Response{ok(), schemaRow(legacySchema)}
		for i := 0; i < position; i++ {
			responses = append(responses, ok())
		}
		responses = append(responses, fail())

		store := mockStore(t, dbmock.Config{Responses: responses})
		if err := store.EnsureSchema(ctx); !errors.Is(err, errMock) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	begin := mockStore(t, dbmock.Config{
		Responses: []dbmock.Response{ok(), schemaRow(legacySchema)},
		BeginErr:  errMock,
	})
	if err := begin.EnsureSchema(ctx); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}
}

func TestUpsertDocumentsReportsEachFailure(t *testing.T) {
	ctx := context.Background()
	files := []storage.FileMetadata{{FileID: "a", AbsolutePath: "/a"}}

	prepare := mockStore(t, dbmock.Config{PrepareErr: errMock, Fallback: ok()})
	if err := prepare.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("prepare: %v", err)
	}

	exec := mockStore(t, dbmock.Config{Responses: []dbmock.Response{fail()}, Fallback: ok()})
	if err := exec.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("exec: %v", err)
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock, Fallback: ok()})
	if err := commit.UpsertDocuments(ctx, files); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}
}

func TestDeleteDocumentReportsEachStatementFailure(t *testing.T) {
	ctx := context.Background()

	chunks := mockStore(t, dbmock.Config{Responses: []dbmock.Response{fail()}, Fallback: ok()})
	if err := chunks.DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("chunks: %v", err)
	}

	documents := mockStore(t, dbmock.Config{Responses: []dbmock.Response{ok(), fail()}, Fallback: ok()})
	if err := documents.DeleteDocument(ctx, 1); !errors.Is(err, errMock) {
		t.Fatalf("documents: %v", err)
	}
}

func TestUpdateDocumentReportsAFailingRowCountAndAMissingRow(t *testing.T) {
	ctx := context.Background()

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

func TestReadPathsReportScanAndIterationFailures(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		columns []string
		row     []driver.Value
		bad     []driver.Value
		call    func(*Store) error
	}{
		{
			name:    "DocumentsFromID",
			columns: []string{"id", "absolute_path"},
			row:     []driver.Value{int64(1), "/a"},
			bad:     []driver.Value{"x", "/a"},
			call:    func(s *Store) error { _, err := s.DocumentsFromID(ctx, 0, 10); return err },
		},
		{
			name:    "DocumentsByIDs",
			columns: []string{"id", "absolute_path"},
			row:     []driver.Value{int64(1), "/a"},
			bad:     []driver.Value{"x", "/a"},
			call:    func(s *Store) error { _, err := s.DocumentsByIDs(ctx, []int64{1}); return err },
		},
		{
			name:    "ChunkDocumentIDs",
			columns: []string{"id", "document_id"},
			row:     []driver.Value{int64(1), int64(2)},
			bad:     []driver.Value{"x", int64(2)},
			call:    func(s *Store) error { _, err := s.ChunkDocumentIDs(ctx, []int64{1}); return err },
		},
		{
			name:    "ChunkMetadataByIDs",
			columns: []string{"id", "document_id", "title", "text"},
			row:     []driver.Value{int64(1), int64(2), "t", "x"},
			bad:     []driver.Value{"x", int64(2), "t", "x"},
			call:    func(s *Store) error { _, err := s.ChunkMetadataByIDs(ctx, []int64{1}); return err },
		},
		{
			name: "ChunksByDocumentID",
			columns: []string{
				"id", "document_id", "chunk_index", "title", "text",
				"token_count", "start_offset", "end_offset", "content_hash",
			},
			row:  []driver.Value{int64(1), int64(2), int64(0), "t", "x", int64(1), int64(0), int64(1), "h"},
			bad:  []driver.Value{"x", int64(2), int64(0), "t", "x", int64(1), int64(0), int64(1), "h"},
			call: func(s *Store) error { _, err := s.ChunksByDocumentID(ctx, 2); return err },
		},
		{
			name: "DocumentsByStatus",
			columns: []string{
				"id", "file_id", "absolute_path", "file_size", "modified_at_ns",
				"content_hash", "scanned_file_size", "scanned_modified_at_ns", "status", "embedded_content_hash",
			},
			row: []driver.Value{int64(1), "f", "/a", int64(1), int64(2), nil, nil, nil, "indexed", nil},
			bad: []driver.Value{"x", "f", "/a", int64(1), int64(2), nil, nil, nil, "indexed", nil},
			call: func(s *Store) error {
				_, err := s.DocumentsByStatus(ctx, "indexed", 0, 10)
				return err
			},
		},
	}

	for _, testCase := range cases {
		mistyped := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
			Columns: testCase.columns, Values: [][]driver.Value{testCase.bad},
		}})
		if err := testCase.call(mistyped); err == nil {
			t.Fatalf("%s: want a scan failure", testCase.name)
		}

		broken := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
			Columns: testCase.columns, Values: [][]driver.Value{testCase.row}, IterErr: errMock,
		}})
		if err := testCase.call(broken); !errors.Is(err, errMock) {
			t.Fatalf("%s iteration: %v", testCase.name, err)
		}
	}
}

func TestApplyDocumentChunkReconcileRunsTheWholePlan(t *testing.T) {
	store := mockStore(t, dbmock.Config{Fallback: ok()})

	inserted, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{
		RemoveIDs: []int64{9},
		Keep:      []storage.Chunk{testChunk()},
		Insert:    []storage.Chunk{testChunk()},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(inserted) != 1 || inserted[0].ID != 5 || inserted[0].DocumentID != 2 {
		t.Fatalf("got %+v", inserted)
	}
}

func TestApplyDocumentChunkReconcileReportsEachStatementFailure(t *testing.T) {
	ctx := context.Background()
	plan := storage.ChunkReconcilePlan{
		RemoveIDs: []int64{9},
		Keep:      []storage.Chunk{testChunk()},
		Insert:    []storage.Chunk{testChunk()},
	}

	begin := mockStore(t, dbmock.Config{BeginErr: errMock, Fallback: ok()})
	if _, err := begin.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
		t.Fatalf("begin: %v", err)
	}

	for position, name := range []string{"delete", "park", "update", "insert"} {
		responses := make([]dbmock.Response, 0, position+1)
		for i := 0; i < position; i++ {
			responses = append(responses, ok())
		}
		responses = append(responses, fail())

		store := mockStore(t, dbmock.Config{Responses: responses, Fallback: ok()})
		if _, err := store.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	commit := mockStore(t, dbmock.Config{CommitErr: errMock, Fallback: ok()})
	if _, err := commit.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
		t.Fatalf("commit: %v", err)
	}

	for position, name := range []string{"update", "park", "insert"} {
		errs := make([]error, position+1)
		errs[position] = errMock

		store := mockStore(t, dbmock.Config{PrepareErrs: errs, Fallback: ok()})
		if _, err := store.ApplyDocumentChunkReconcile(ctx, 2, plan); !errors.Is(err, errMock) {
			t.Fatalf("%s prepare: %v", name, err)
		}
	}
}

func TestApplyDocumentChunkReconcileReportsAFailingInsertID(t *testing.T) {
	store := mockStore(t, dbmock.Config{Fallback: dbmock.Response{
		Result: dbmock.Result{LastInsertIDErr: errMock},
	}})

	_, err := store.ApplyDocumentChunkReconcile(context.Background(), 2, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{testChunk()},
	})
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenStorageOpensAndPreparesTheSchema(t *testing.T) {
	store, err := OpenStorage(context.Background(), filepath.Join(t.TempDir(), "index.db"))
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

	store, err := OpenStorage(context.Background(), "x.db")
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
		return dbmock.Open(dbmock.Config{Responses: []dbmock.Response{ok(), fail()}}), nil
	}
	defer func() { openDB = original }()

	store, err := OpenStorage(context.Background(), "x.db")
	if !errors.Is(err, errMock) {
		t.Fatalf("got %v", err)
	}
	if store != nil {
		t.Fatal("want a nil storage.Storage")
	}
}
