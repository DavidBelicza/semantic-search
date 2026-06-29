package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/crawler"
	"semantic-search/internal/storage"
)

func TestNewIndexCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	indexCmd := NewIndexCommand(&out, &fakeDocumentStore{})
	indexCmd.SetArgs([]string{"--help"})

	if err := indexCmd.Execute(); err != nil {
		t.Fatalf("execute index help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "index [path]") {
		t.Fatalf("help output does not contain index usage: %q", help)
	}
}

func TestNewIndexCommandRequiresPath(t *testing.T) {
	var out bytes.Buffer
	indexCmd := NewIndexCommand(&out, &fakeDocumentStore{})
	indexCmd.SetArgs([]string{})

	if err := indexCmd.Execute(); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestNewIndexCommandStoresMetadataAndScansContent(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	nested := filepath.Join(root, "notes", "daily")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}

	readmeFile := filepath.Join(root, "README.md")
	planFile := filepath.Join(root, "notes", "plan.md")
	entryFile := filepath.Join(nested, "entry.md")
	ignoredFile := filepath.Join(root, "ignore.txt")

	files := []string{readmeFile, planFile, entryFile, ignoredFile}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatalf("write test file %q: %v", file, err)
		}
	}

	var out bytes.Buffer
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	indexCmd := NewIndexCommand(&out, store)
	indexCmd.SetArgs([]string{root})

	if err := indexCmd.Execute(); err != nil {
		t.Fatalf("execute index: %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("expected no index output, got %q", out.String())
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
	if count != 3 {
		t.Fatalf("document count mismatch: want 3, got %d", count)
	}

	var chunkedCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE content_hash IS NOT NULL AND status = ?", storage.DocumentStatusChunked).Scan(&chunkedCount); err != nil {
		t.Fatalf("count chunked documents: %v", err)
	}
	if chunkedCount != 3 {
		t.Fatalf("chunked document count mismatch: want 3, got %d", chunkedCount)
	}

	var chunkCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", chunkCount)
	}
}

type fakeDocumentStore struct{}

func (s *fakeDocumentStore) UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error {
	return nil
}

func (s *fakeDocumentStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
	return nil, nil
}

func (s *fakeDocumentStore) UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error {
	return nil
}

func (s *fakeDocumentStore) UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error {
	return nil
}

func (s *fakeDocumentStore) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error {
	return nil
}
