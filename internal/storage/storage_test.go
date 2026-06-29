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

	root := filepath.Clean("/tmp/docs")
	first := crawler.FileMetadata{
		RootPath:     root,
		RelativePath: "README.md",
		AbsolutePath: filepath.Join(root, "README.md"),
		SizeBytes:    10,
		ModifiedAtNS: 100,
	}
	second := crawler.FileMetadata{
		RootPath:     root,
		RelativePath: filepath.Join("notes", "plan.md"),
		AbsolutePath: filepath.Join(root, "notes", "plan.md"),
		SizeBytes:    20,
		ModifiedAtNS: 200,
	}

	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{first, second}); err != nil {
		t.Fatalf("insert documents: %v", err)
	}

	first.SizeBytes = 15
	first.ModifiedAtNS = 150
	if err := store.UpsertDocuments(ctx, []crawler.FileMetadata{first}); err != nil {
		t.Fatalf("update document: %v", err)
	}

	rows, err := store.db.QueryContext(ctx, `
SELECT relative_path, file_size, modified_at_ns
FROM documents
ORDER BY relative_path`)
	if err != nil {
		t.Fatalf("query documents: %v", err)
	}
	defer rows.Close()

	got := map[string]struct {
		size       int64
		modifiedNS int64
	}{}
	for rows.Next() {
		var relativePath string
		var size int64
		var modifiedNS int64
		if err := rows.Scan(&relativePath, &size, &modifiedNS); err != nil {
			t.Fatalf("scan document: %v", err)
		}
		got[relativePath] = struct {
			size       int64
			modifiedNS int64
		}{size: size, modifiedNS: modifiedNS}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate documents: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("document count mismatch: want 2, got %d", len(got))
	}
	if got["README.md"].size != 15 || got["README.md"].modifiedNS != 150 {
		t.Fatalf("README.md was not updated: %#v", got["README.md"])
	}
	if got[filepath.Join("notes", "plan.md")].size != 20 {
		t.Fatalf("plan.md changed unexpectedly: %#v", got[filepath.Join("notes", "plan.md")])
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
