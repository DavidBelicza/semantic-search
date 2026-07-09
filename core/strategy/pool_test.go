package strategy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

// fakeStrategy claims by extension; an empty ext claims every file (a catch-all).
type fakeStrategy struct {
	ext string
}

func (f fakeStrategy) Claims(path string) bool {
	if f.ext == "" {
		return true
	}
	return strings.EqualFold(filepath.Ext(path), f.ext)
}

func (fakeStrategy) CreateMetadata(FileRef) (storage.FileMetadata, error) {
	return storage.FileMetadata{}, nil
}
func (fakeStrategy) Fingerprint([]byte) string                    { return "" }
func (fakeStrategy) Parse([]byte) (ParsedDocument, error)         { return ParsedDocument{}, nil }
func (fakeStrategy) Chunk(storage.Document, ParsedDocument) ([]storage.Chunk, error) {
	return nil, nil
}
func (fakeStrategy) Embed(context.Context, []storage.Chunk) ([][]float32, error) { return nil, nil }

func TestPoolForReturnsClaimingStrategy(t *testing.T) {
	pool := NewPool(fakeStrategy{ext: ".md"})

	if _, ok := pool.For("note.md"); !ok {
		t.Fatal("expected a strategy for note.md")
	}
	if _, ok := pool.For("note.txt"); ok {
		t.Fatal("expected no strategy for note.txt")
	}
}

func TestPoolForFallsThroughToCatchAll(t *testing.T) {
	pool := NewPool(fakeStrategy{ext: ".md"}, fakeStrategy{ext: ""})

	if _, ok := pool.For("note.txt"); !ok {
		t.Fatal("catch-all strategy should claim note.txt")
	}
}
