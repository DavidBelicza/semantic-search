package indexer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/crawler"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/strategy"
)

func TestIndexPathStoresMetadata(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	nested := filepath.Join(root, "notes")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}

	readmeFile := filepath.Join(root, "README.md")
	planFile := filepath.Join(nested, "plan.md")
	ignoredFile := filepath.Join(root, "ignore.txt")
	for _, file := range []string{readmeFile, planFile, ignoredFile} {
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatalf("write file %q: %v", file, err)
		}
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if err := IndexPath(context.Background(), store, root, strategy.DefaultPool(), crawler.Options{}); err != nil {
		t.Fatalf("index path: %v", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if count != 2 {
		t.Fatalf("document count mismatch: want 2, got %d", count)
	}

	var indexedCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE status = ?", storage.DocumentStatusIndexed).Scan(&indexedCount); err != nil {
		t.Fatalf("count indexed documents: %v", err)
	}
	if indexedCount != 2 {
		t.Fatalf("indexed document count mismatch: want 2, got %d", indexedCount)
	}
}

func TestUpsertDocumentsInBatchesUsesFixedBatchSize(t *testing.T) {
	files := make([]crawler.FileMetadata, documentUpsertBatchSize*2+1)
	store := &recordingDocumentStore{}

	if err := upsertDocumentsInBatches(context.Background(), store, files); err != nil {
		t.Fatalf("upsert documents in batches: %v", err)
	}

	want := []int{documentUpsertBatchSize, documentUpsertBatchSize, 1}
	if !reflect.DeepEqual(store.batchSizes, want) {
		t.Fatalf("batch sizes mismatch\nwant: %#v\n got: %#v", want, store.batchSizes)
	}
}

func TestSupportedFilesFiltersUnsupportedFiles(t *testing.T) {
	files := []crawler.FileMetadata{
		{AbsolutePath: "/tmp/a.md"},
		{AbsolutePath: "/tmp/b.txt"},
		{AbsolutePath: "/tmp/c.markdown"},
	}

	got := supportedFiles(files, strategy.DefaultPool())
	if len(got) != 2 {
		t.Fatalf("supported file count mismatch: want 2, got %d", len(got))
	}
	if got[0].AbsolutePath != "/tmp/a.md" || got[1].AbsolutePath != "/tmp/c.markdown" {
		t.Fatalf("supported files mismatch: %#v", got)
	}
}

type recordingDocumentStore struct {
	batchSizes []int
}

func (s *recordingDocumentStore) UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error {
	s.batchSizes = append(s.batchSizes, len(files))
	return nil
}
