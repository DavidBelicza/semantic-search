package semanticsearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
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

// compile-time check that the fake satisfies the embedder contract.
var _ strategy.Embedder = fixedEmbedder{}

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

// --- Engine ---

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

// --- Embedder ---

func TestNewAiEmbedderOpenAI(t *testing.T) {
	e := NewAiEmbedder(AiEmbedderConfig{
		Standard:   StandardOpenAI,
		BaseURL:    "http://127.0.0.1:1234",
		Model:      "embeddinggemma-300m",
		Dimensions: 768,
	})
	if e == nil {
		t.Fatal("expected an embedder for the OpenAI standard")
	}
}

func TestNewAiEmbedderUnknownStandardIsNil(t *testing.T) {
	if NewAiEmbedder(AiEmbedderConfig{Standard: "nope"}) != nil {
		t.Fatal("expected nil for an unknown standard")
	}
}

// --- Storage ---

func TestNewSQLiteStorageOpens(t *testing.T) {
	store, err := NewSQLiteStorage(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open sqlite storage: %v", err)
	}
	defer store.Close()
}

func TestNewSQLiteVectorStorageOpens(t *testing.T) {
	store, err := NewSQLiteVectorStorage(context.Background(), filepath.Join(t.TempDir(), "vectors.db"), 8)
	if err != nil {
		t.Fatalf("open sqlite-vec storage: %v", err)
	}
	defer store.Close()
}

// --- Search helpers ---

func TestBuildSearchResultsResolvesInHitOrder(t *testing.T) {
	hits := []storage.VectorHit{
		{ChunkID: 7, Distance: 0.5},
		{ChunkID: 9, Distance: 0.8},
	}
	metadata := []storage.ChunkMetadata{
		{ChunkID: 9, DocumentID: 2, Title: "Refunds", Text: "refund the payment"},
		{ChunkID: 7, DocumentID: 42, Title: "Payments", Text: "pay the invoice"},
	}

	results := buildSearchResults(hits, metadata)

	if len(results) != 2 {
		t.Fatalf("result count mismatch: %d", len(results))
	}
	if got := results[0]; got.ChunkID != 7 || got.DocumentID != 42 || got.Title != "Payments" || got.Text != "pay the invoice" || got.Score != 0.5 {
		t.Fatalf("first result mismatch: %#v", got)
	}
	if results[1].ChunkID != 9 {
		t.Fatalf("hit order not preserved: %#v", results)
	}
}

func TestBuildSearchResultsSkipsMissingMetadata(t *testing.T) {
	hits := []storage.VectorHit{{ChunkID: 1, Distance: 0.1}}
	if results := buildSearchResults(hits, nil); len(results) != 0 {
		t.Fatalf("expected no results when metadata is missing, got %d", len(results))
	}
}

func TestHitChunkIDs(t *testing.T) {
	ids := hitChunkIDs([]storage.VectorHit{{ChunkID: 3}, {ChunkID: 5}})
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 5 {
		t.Fatalf("chunk ids mismatch: %v", ids)
	}
}
