package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"semantic-search/internal/crawler"
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
	if status != DocumentStatusIndexed {
		t.Fatalf("status mismatch: want indexed, got %q", status)
	}

	if _, err := store.db.ExecContext(ctx, "UPDATE documents SET status = ? WHERE file_id = '1:100'", DocumentStatusEmbedded); err != nil {
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

	exists, err := store.documentsColumnExists(ctx, "embedded_content_hash")
	if err != nil {
		t.Fatalf("check column: %v", err)
	}
	if !exists {
		t.Fatal("expected embedded_content_hash column to be added to legacy database")
	}

	if err := store.MarkDocumentEmbedded(ctx, "1:100", "content-hash"); err != nil {
		t.Fatalf("mark embedded: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, DocumentStatusEmbedded, 0, 10)
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
	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{{
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

	initial := ChunkReconcilePlan{Insert: []Chunk{
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

	incoming := []Chunk{
		{ChunkIndex: 0, Text: "B", TokenCount: 1, StartOffset: 0, EndOffset: 1, ContentHash: "hash-b"},
		{ChunkIndex: 1, Text: "A", TokenCount: 1, StartOffset: 1, EndOffset: 2, ContentHash: "hash-a"},
	}
	plan := ReconcileChunks(existing, incoming)
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

	first := crawler.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	second := crawler.FileMetadata{
		FileID:       "1:200",
		AbsolutePath: filepath.Clean("/tmp/docs/notes/plan.md"),
		SizeBytes:    20,
		ModifiedAtNS: 200,
	}

	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{first, second}); err != nil {
		t.Fatalf("insert documents: %v", err)
	}

	if err := store.UpdateDocumentStatus(ctx, second.FileID, DocumentStatusEmbedded); err != nil {
		t.Fatalf("mark second embedded: %v", err)
	}

	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{first, second}); err != nil {
		t.Fatalf("upsert unchanged documents: %v", err)
	}

	first.SizeBytes = 15
	first.ModifiedAtNS = 150
	first.AbsolutePath = filepath.Clean("/tmp/docs/README-renamed.md")
	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{first}); err != nil {
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
	if got["1:100"].status != DocumentStatusIndexed {
		t.Fatalf("first document status mismatch: want indexed, got %q", got["1:100"].status)
	}
	if got["1:200"].size != 20 {
		t.Fatalf("second document changed unexpectedly: %#v", got["1:200"])
	}
	if got["1:200"].status != DocumentStatusEmbedded {
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

	file := crawler.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{file}); err != nil {
		t.Fatalf("insert document: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, DocumentStatusIndexed, 0, 10)
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
	if err := store.UpdateDocumentContentHashAndStatus(ctx, file.FileID, wantHash, DocumentStatusScanned); err != nil {
		t.Fatalf("update content hash and status: %v", err)
	}

	documents, err = store.DocumentsByStatus(ctx, DocumentStatusScanned, 0, 10)
	if err != nil {
		t.Fatalf("documents by scanned status: %v", err)
	}
	if len(documents) != 1 || !documents[0].HasHash || documents[0].ContentHash != wantHash {
		t.Fatalf("scanned document mismatch: %#v", documents)
	}

	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, file.FileID, DocumentStatusEmbedded); err != nil {
		t.Fatalf("update scan checkpoint and status: %v", err)
	}

	documents, err = store.DocumentsByStatus(ctx, DocumentStatusEmbedded, 0, 10)
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

	file := crawler.FileMetadata{
		FileID:       "1:100",
		AbsolutePath: filepath.Clean("/tmp/docs/README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{file}); err != nil {
		t.Fatalf("insert document: %v", err)
	}

	documents, err := store.DocumentsByStatus(ctx, DocumentStatusIndexed, 0, 1)
	if err != nil {
		t.Fatalf("documents by status: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("document count mismatch: want 1, got %d", len(documents))
	}

	chunks := []Chunk{
		{ChunkIndex: 0, Text: "hello", TokenCount: 2, StartOffset: 0, EndOffset: 5, ContentHash: "hash-1"},
		{ChunkIndex: 1, Text: "world", TokenCount: 2, StartOffset: 5, EndOffset: 10, ContentHash: "hash-2"},
	}
	initialPlan := ReconcileChunks(nil, chunks)
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

	nextChunks := []Chunk{
		{ChunkIndex: 0, Text: "hello", TokenCount: 2, StartOffset: 0, EndOffset: 5, ContentHash: "hash-1"},
		{ChunkIndex: 1, Text: "new", TokenCount: 1, StartOffset: 5, EndOffset: 8, ContentHash: "hash-3"},
	}
	plan := ReconcileChunks(existing, nextChunks)
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
	existing := []Chunk{
		{ID: 10, DocumentID: 42, ChunkIndex: 0, ContentHash: "same"},
		{ID: 11, DocumentID: 42, ChunkIndex: 1, ContentHash: "same"},
		{ID: 12, DocumentID: 42, ChunkIndex: 2, ContentHash: "removed"},
	}
	incoming := []Chunk{
		{ChunkIndex: 0, ContentHash: "same"},
		{ChunkIndex: 1, ContentHash: "same"},
		{ChunkIndex: 2, ContentHash: "new"},
	}

	plan := ReconcileChunks(existing, incoming)

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
