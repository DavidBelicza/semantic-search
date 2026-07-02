// Package reader provides generic, format-agnostic file reading used by strategies.
// These are plain functions, not methods, because the behaviour is shared across all
// formats.
package reader

import (
	"context"
	"io"
	"os"

	storage "semantic-search/internal/storage/sqlite"
)

// ReadFile reads a document's file and returns its contents as text. It is generic:
// any text-based format (Markdown, txt, log, …) can use it.
func ReadFile(ctx context.Context, document storage.Document) (string, error) {
	file, err := os.Open(document.AbsolutePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
