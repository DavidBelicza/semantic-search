package reader

import (
	"context"
	"io"
	"os"

	"semantic-search/internal/storage"
)

type MarkdownReader struct{}

func (r MarkdownReader) Read(ctx context.Context, document storage.Document) (string, error) {
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
