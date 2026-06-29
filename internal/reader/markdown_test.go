package reader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/storage"
)

func TestMarkdownReaderReadsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := (MarkdownReader{}).Read(context.Background(), storage.Document{AbsolutePath: path})
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}

	if got != "hello" {
		t.Fatalf("content mismatch: want hello, got %q", got)
	}
}
