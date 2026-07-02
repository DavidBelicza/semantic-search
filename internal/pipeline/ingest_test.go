package pipeline

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/ingest"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/strategy"
)

func TestIngestDiscoversRegistersAndFingerprints(t *testing.T) {
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

	pool := strategy.NewPool(strategy.NewMarkdownStrategy())
	if err := Ingest(context.Background(), store, pool, root, ingest.Options{}, false); err != nil {
		t.Fatalf("ingest: %v", err)
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
		t.Fatalf("document count mismatch: want 2 (the .txt is unsupported), got %d", count)
	}

	// Discovery + registration leave documents indexed; fingerprinting then hashes the
	// existing files and advances them to scanned.
	var scannedCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE status = ?", storage.DocumentStatusScanned).Scan(&scannedCount); err != nil {
		t.Fatalf("count scanned documents: %v", err)
	}
	if scannedCount != 2 {
		t.Fatalf("scanned document count mismatch: want 2, got %d", scannedCount)
	}
}
