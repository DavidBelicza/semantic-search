package storage

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

	documents, err := store.DocumentsByStatus(ctx, DocumentStatusIndexed, 10)
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

	documents, err = store.DocumentsByStatus(ctx, DocumentStatusScanned, 10)
	if err != nil {
		t.Fatalf("documents by scanned status: %v", err)
	}
	if len(documents) != 1 || !documents[0].HasHash || documents[0].ContentHash != wantHash {
		t.Fatalf("scanned document mismatch: %#v", documents)
	}

	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, file.FileID, DocumentStatusDone); err != nil {
		t.Fatalf("update scan checkpoint and status: %v", err)
	}

	documents, err = store.DocumentsByStatus(ctx, DocumentStatusDone, 10)
	if err != nil {
		t.Fatalf("documents by done status: %v", err)
	}
	if len(documents) != 1 || !documents[0].HasScannedMetadata {
		t.Fatalf("done document mismatch: %#v", documents)
	}
}

func TestReplaceDocumentChunksAndStatus(t *testing.T) {
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

	documents, err := store.DocumentsByStatus(ctx, DocumentStatusIndexed, 1)
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
	if err := store.ReplaceDocumentChunksAndStatus(ctx, documents[0].ID, chunks, DocumentStatusChunked); err != nil {
		t.Fatalf("replace chunks: %v", err)
	}

	var chunkCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks WHERE document_id = ?", documents[0].ID).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 2 {
		t.Fatalf("chunk count mismatch: want 2, got %d", chunkCount)
	}

	documents, err = store.DocumentsByStatus(ctx, DocumentStatusChunked, 1)
	if err != nil {
		t.Fatalf("documents by chunked status: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("chunked document count mismatch: want 1, got %d", len(documents))
	}

	if err := store.ReplaceDocumentChunksAndStatus(ctx, documents[0].ID, chunks[:1], DocumentStatusChunked); err != nil {
		t.Fatalf("replace chunks second time: %v", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks WHERE document_id = ?", documents[0].ID).Scan(&chunkCount); err != nil {
		t.Fatalf("count replaced chunks: %v", err)
	}
	if chunkCount != 1 {
		t.Fatalf("replaced chunk count mismatch: want 1, got %d", chunkCount)
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
