package semanticsearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

// fixedEmbedder returns the same unit vector for every input, so an index→search round-trip
// is deterministic without a real embedding server.
type fixedEmbedder struct{}

func (fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0, 0, 0, 0, 0, 0, 0}
	}
	return vectors, nil
}

func newTestEngine(t *testing.T, factories ...StrategyFactory) *Engine {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	store, err := NewSQLiteStorage(ctx, filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	vectors, err := NewSQLiteVectorStorage(ctx, filepath.Join(dir, "vectors.db"), 8)
	if err != nil {
		t.Fatalf("open vector storage: %v", err)
	}

	engine, err := NewEngine(Config{
		Embedder:      fixedEmbedder{},
		Storage:       store,
		VectorStorage: vectors,
		Strategies:    factories,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
		vectors.Close()
	})
	return engine
}

func TestNewEngineRejectsDuplicateExtensions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, _ := NewSQLiteStorage(ctx, filepath.Join(dir, "a.db"))
	vectors, _ := NewSQLiteVectorStorage(ctx, filepath.Join(dir, "b.db"), 8)
	defer store.Close()
	defer vectors.Close()

	_, err := NewEngine(Config{
		Embedder:      fixedEmbedder{},
		Storage:       store,
		VectorStorage: vectors,
		Strategies:    []StrategyFactory{NewMarkdownStrategy(), NewMarkdownStrategy()},
	})
	if err == nil {
		t.Fatal("expected duplicate-extension error")
	}
}

func TestNewEngineRequiresDependencies(t *testing.T) {
	if _, err := NewEngine(Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := NewEngine(Config{Embedder: fixedEmbedder{}}); err == nil {
		t.Fatal("expected error for missing storage")
	}
}

func TestEngineIndexAndSearch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("The vacation policy grants fifteen paid days."), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := newTestEngine(t, NewTextStrategy())
	ctx := context.Background()

	if err := engine.Index(ctx, dir, IndexOptions{}); err != nil {
		t.Fatalf("index: %v", err)
	}

	results, err := engine.Search(ctx, "vacation", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Text == "" {
		t.Fatal("result text should be populated")
	}
}

// compile-time check that the built-ins produce the expected interface.
var _ strategy.Embedder = fixedEmbedder{}
