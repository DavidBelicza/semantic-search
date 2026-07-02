package reader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
)

func TestReadFileReadsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := ReadFile(context.Background(), storage.Document{AbsolutePath: path})
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if got != "hello" {
		t.Fatalf("content mismatch: want hello, got %q", got)
	}
}
