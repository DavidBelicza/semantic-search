package semanticsearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
var _ strategy.AiClient = fixedEmbedder{}

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
		Model:         NewModel(Gemma300mQAT),
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

	results, err := engine.Search(ctx, SearchConfig{Query: "vacation"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one document")
	}
	if results[0].FileName != "notes.txt" {
		t.Fatalf("document file name not resolved: %q", results[0].FileName)
	}
	if len(results[0].Chunks) == 0 || results[0].Chunks[0].Text == "" {
		t.Fatal("document chunk text should be populated")
	}
}

func TestEngineIndexPrunesMissingFilesByDefault(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	remove := filepath.Join(dir, "remove.txt")
	if err := os.WriteFile(keep, []byte("The vacation policy grants fifteen paid days."), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(remove, []byte("The office is closed on public holidays."), 0o644); err != nil {
		t.Fatalf("write remove: %v", err)
	}

	engine := newTestEngine(t, NewTextStrategy())
	ctx := context.Background()

	if err := engine.Index(ctx, dir, IndexOptions{}); err != nil {
		t.Fatalf("index: %v", err)
	}
	if got := documentNames(t, engine); len(got) != 2 {
		t.Fatalf("expected 2 documents indexed, got %v", got)
	}

	if err := os.Remove(remove); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	// Default re-index prunes the deleted file.
	if err := engine.Index(ctx, dir, IndexOptions{}); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if names := documentNames(t, engine); len(names) != 1 || names[0] != "keep.txt" {
		t.Fatalf("expected only keep.txt after default prune, got %v", names)
	}
}

func TestEngineKeepMissingFilesRetainsDeleted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("The vacation policy grants fifteen paid days."), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := newTestEngine(t, NewTextStrategy())
	ctx := context.Background()

	if err := engine.Index(ctx, dir, IndexOptions{}); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	// KeepMissingFiles skips the prune, so the deleted file's document survives.
	if err := engine.Index(ctx, dir, IndexOptions{KeepMissingFiles: true}); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if names := documentNames(t, engine); len(names) != 1 {
		t.Fatalf("expected the deleted file's document retained, got %v", names)
	}
}

// documentNames returns the file names of every document a broad search surfaces.
func documentNames(t *testing.T, engine *Engine) []string {
	t.Helper()
	results, err := engine.Search(context.Background(), SearchConfig{Query: "policy office vacation holidays"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	names := make([]string, 0, len(results))
	for _, doc := range results {
		names = append(names, doc.FileName)
	}
	return names
}

// --- Embedder ---

func TestNewAiEmbedderOpenAI(t *testing.T) {
	e := NewAiEmbedder(AiEmbedderConfig{
		Standard: StandardOpenAI,
		BaseURL:  "http://127.0.0.1:1234",
	}, NewModel(Gemma300mQAT))
	if e == nil {
		t.Fatal("expected an embedder for the OpenAI standard")
	}
}

func TestNewAiEmbedderUnknownStandardIsNil(t *testing.T) {
	if NewAiEmbedder(AiEmbedderConfig{Standard: "nope"}, NewModel(Gemma300mQAT)) != nil {
		t.Fatal("expected nil for an unknown standard")
	}
}

func TestNewModelUnlistedReturnsGeneralModel(t *testing.T) {
	m := NewModel("text-embedding-nomic-embed-text-v1.5", 768)
	if m == nil {
		t.Fatal("expected a general model for an unlisted model id")
	}
	if m.Name() != "text-embedding-nomic-embed-text-v1.5" || m.Dimensions() != 768 {
		t.Fatalf("general model not configured: name=%q dims=%d", m.Name(), m.Dimensions())
	}
}

func TestNewGeneralModel(t *testing.T) {
	m := NewGeneralModel("text-embedding-nomic-embed-text-v1.5", 512)
	if m.Name() != "text-embedding-nomic-embed-text-v1.5" || m.Dimensions() != 512 {
		t.Fatalf("general model not configured: name=%q dims=%d", m.Name(), m.Dimensions())
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

func TestNewModelPredefinedConstants(t *testing.T) {
	for _, id := range []PredefinedModel{Gemma300mQAT, Nomic768, E5Large1024, BGELarge1024, Qwen30_6B1024, MxbaiLarge1024} {
		m := NewModel(id)
		if m == nil || m.Name() == "" || m.Dimensions() <= 0 {
			t.Fatalf("predefined model %q not configured", id)
		}
	}
}

func TestStrategyFactoriesBuild(t *testing.T) {
	model := NewModel(Gemma300mQAT)
	embedder := fixedEmbedder{}

	factories := []StrategyFactory{
		NewMarkdownStrategy(),
		NewPDFStrategy(),
		NewCodeStrategy(),
		NewDocxStrategy(),
		NewTextStrategy(),
	}
	for _, factory := range factories {
		if len(factory.Extensions) == 0 || factory.Build == nil {
			t.Fatalf("factory not initialized: %#v", factory.Extensions)
		}
		strat, release, err := factory.Build(model, embedder)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if strat == nil {
			t.Fatal("build returned a nil strategy")
		}
		if release != nil {
			_ = release()
		}
	}
}

func TestValidateConfigRequiresEachDependency(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, _ := NewSQLiteStorage(ctx, filepath.Join(dir, "a.db"))
	defer store.Close()
	vectors, _ := NewSQLiteVectorStorage(ctx, filepath.Join(dir, "b.db"), 8)
	defer vectors.Close()

	full := Config{
		Model:         NewModel(Gemma300mQAT),
		Embedder:      fixedEmbedder{},
		Storage:       store,
		VectorStorage: vectors,
		Strategies:    []StrategyFactory{NewTextStrategy()},
	}

	cases := map[string]func(Config) Config{
		"model":         func(c Config) Config { c.Model = nil; return c },
		"embedder":      func(c Config) Config { c.Embedder = nil; return c },
		"storage":       func(c Config) Config { c.Storage = nil; return c },
		"vectorStorage": func(c Config) Config { c.VectorStorage = nil; return c },
		"strategies":    func(c Config) Config { c.Strategies = nil; return c },
	}
	for name, mutate := range cases {
		if _, err := NewEngine(mutate(full)); err == nil {
			t.Fatalf("expected an error when %s is missing", name)
		}
	}

	if _, err := NewEngine(full); err != nil {
		t.Fatalf("the full config should be valid: %v", err)
	}
}

func TestNewSQLiteStorageErrorsOnBadPath(t *testing.T) {
	if _, err := NewSQLiteStorage(context.Background(), "/no/such/dir/index.db"); err == nil {
		t.Fatal("expected an error for an unwritable path")
	}
}

func TestNewSQLiteVectorStorageErrorsOnBadDimensions(t *testing.T) {
	if _, err := NewSQLiteVectorStorage(context.Background(), filepath.Join(t.TempDir(), "v.db"), 0); err == nil {
		t.Fatal("expected an error for zero dimensions")
	}
}

func TestNewAiEmbedderWithTimeout(t *testing.T) {
	e := NewAiEmbedder(AiEmbedderConfig{
		Standard: StandardOpenAI,
		BaseURL:  "http://127.0.0.1:1234",
		Timeout:  time.Second,
	}, NewModel(Gemma300mQAT))
	if e == nil {
		t.Fatal("expected an embedder")
	}
}

func TestNewPostgresConstructors(t *testing.T) {
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run postgres constructor tests")
	}
	ctx := context.Background()

	store, err := NewPostgresStorage(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres storage: %v", err)
	}
	defer store.Close()

	vectors, err := NewPostgresVectorStorage(ctx, dsn, 8, PostgresKNN)
	if err != nil {
		t.Fatalf("pgvector storage: %v", err)
	}
	defer vectors.Close()
}

func TestEngineIndexPropagatesStrategyBuildError(t *testing.T) {
	failing := StrategyFactory{
		Extensions: []string{".xyz"},
		Build: func(strategy.EmbeddingModel, strategy.AiClient) (strategy.Strategy, func() error, error) {
			return nil, nil, errors.New("build boom")
		},
	}
	engine := newTestEngine(t, failing)
	if err := engine.Index(context.Background(), t.TempDir(), IndexOptions{}); err == nil {
		t.Fatal("expected the strategy build error to propagate")
	}
}

func TestEngineSearchRejectsEmptyQuery(t *testing.T) {
	engine := newTestEngine(t, NewTextStrategy())
	if _, err := engine.Search(context.Background(), SearchConfig{Query: "   "}); err == nil {
		t.Fatal("expected an error for a blank query")
	}
}

func TestNewModelUnlistedWithDimensions(t *testing.T) {
	m := NewModel("some-custom-model", 512)
	if m.Name() != "some-custom-model" || m.Dimensions() != 512 {
		t.Fatalf("unlisted model not configured: name=%q dims=%d", m.Name(), m.Dimensions())
	}
}
