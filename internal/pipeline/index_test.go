package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/pipeline"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/strategy"
)

func TestIndexDiscoversRegistersAndFingerprints(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	nested := filepath.Join(root, "notes")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, file := range []string{
		filepath.Join(root, "README.md"),
		filepath.Join(nested, "plan.md"),
		filepath.Join(root, "ignore.txt"),
	} {
		if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
			t.Fatalf("write %q: %v", file, err)
		}
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}

	pool := strategy.NewPool(strategy.NewMarkdownStrategy(strategy.NewGeneralStrategy(nil)))
	if err := pipeline.Index(context.Background(), store, pool, root, pipeline.Options{}, false); err != nil {
		t.Fatalf("index: %v", err)
	}

	docs, err := store.DocumentsByStatus(context.Background(), storage.DocumentStatusScanned, 0, 100)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 scanned markdown docs (the .txt is unclaimed), got %d", len(docs))
	}
}
