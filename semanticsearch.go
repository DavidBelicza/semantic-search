// Package semanticsearch is the library entry point: it composes an embedder, a metadata
// store, a vector store, and a set of strategies into an Engine that indexes a directory and
// answers meaning-based queries. Every dependency is an interface, so callers can use the
// built-in implementations (the NewXxx constructors) or supply their own.
package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/davidbelicza/semantic-search/core/embedder"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/storage/sqlite"
	"github.com/davidbelicza/semantic-search/core/storage/sqlitevec"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/code"
	"github.com/davidbelicza/semantic-search/core/strategy/docx"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/core/strategy/markdown"
	"github.com/davidbelicza/semantic-search/core/strategy/pdf"
	"github.com/davidbelicza/semantic-search/internal/pipeline"
)

// --- Engine ---

// Config is the injected object graph for an Engine: the embedder, the two stores, and the
// strategies. Each field is required. Every dependency is an interface, so a caller can supply
// the built-in implementations (via the NewXxx constructors) or their own.
type Config struct {
	Embedder      strategy.Embedder
	Storage       storage.Storage
	VectorStorage storage.VectorStorage
	Strategies    []StrategyFactory
}

// Engine is a configured index/search unit. Multiple engines with different embedders, stores,
// and strategies can run independently and in parallel.
type Engine struct {
	embedder    strategy.Embedder
	store       storage.Storage
	vectorStore storage.VectorStorage
	factories   []StrategyFactory
}

// NewEngine validates the config and composes the engine. It errors on a missing dependency or
// when two strategies claim the same extension. Strategies are built per Index run (that is
// the only place they are used), so their resources live no longer than indexing needs them.
func NewEngine(config Config) (*Engine, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return &Engine{
		embedder:    config.Embedder,
		store:       config.Storage,
		vectorStore: config.VectorStorage,
		factories:   config.Strategies,
	}, nil
}

// Index runs the two pipelines: discover → register → fingerprint, then read → parse → chunk →
// embed. Re-running is incremental: unchanged files are not re-embedded. The strategies (and
// any resources they open, like the PDF extractor) are built here and released when indexing
// finishes.
func (e *Engine) Index(ctx context.Context, rootPath string, options IndexOptions) error {
	strategies, release, err := buildStrategies(e.factories, e.embedder)
	if err != nil {
		return err
	}
	defer release()

	pool := strategy.NewPool(strategies...)
	walkOptions := pipeline.Options{
		IncludeHidden:  options.IncludeHidden,
		FollowSymlinks: options.FollowSymlinks,
	}

	if err := pipeline.Index(ctx, e.store, pool, rootPath, walkOptions, options.FailFast); err != nil {
		return err
	}

	return pipeline.Process(ctx, e.store, e.vectorStore, pool, options.FailFast)
}

// Search embeds the query and returns the nearest chunk matches in similarity order.
func (e *Engine) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be greater than zero")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search query is required")
	}

	vectors, err := e.embedder.Embed(ctx, []string{embedder.QueryPrefix + query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("expected one query embedding, got %d", len(vectors))
	}

	hits, err := e.vectorStore.Search(ctx, vectors[0], limit)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	metadata, err := e.store.ChunkMetadataByIDs(ctx, hitChunkIDs(hits))
	if err != nil {
		return nil, err
	}

	return buildSearchResults(hits, metadata), nil
}

// IndexOptions configures an index run.
type IndexOptions struct {
	// FailFast aborts on the first per-document error instead of collecting and continuing.
	FailFast bool
	// IncludeHidden indexes hidden files and directories.
	IncludeHidden bool
	// FollowSymlinks resolves and indexes symlink targets.
	FollowSymlinks bool
}

// SearchResult is one chunk match: the document it belongs to, the chunk id, its title
// and text, and the score (vector distance from the query — lower is closer).
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

// --- Embedder ---

// Standard identifies the wire protocol an AI embedder speaks. Most embedding servers are
// OpenAI-compatible; other standards can be added later without changing callers.
type Standard string

// StandardOpenAI is the OpenAI-compatible /v1/embeddings protocol (LM Studio, Ollama, and
// most local servers).
const StandardOpenAI Standard = "openai"

// AiEmbedderConfig configures an AI embedder: which protocol to speak, and the endpoint,
// model, and vector size to use.
type AiEmbedderConfig struct {
	Standard   Standard
	BaseURL    string
	Model      string
	Dimensions int
}

// NewAiEmbedder builds an embedder for the given standard. It returns nil for an unknown
// standard; NewEngine rejects a nil embedder.
func NewAiEmbedder(config AiEmbedderConfig) strategy.Embedder {
	if config.Standard != StandardOpenAI {
		return nil
	}

	client := embedder.NewOpenAIEmbedder(config.BaseURL, config.Model)
	if config.Dimensions > 0 {
		client.Dimensions = config.Dimensions
	}

	return client
}

// --- Storage ---

// NewSQLiteStorage opens a SQLite metadata store at path and prepares its schema. The returned
// value is the injectable storage.Storage; a caller can implement that interface instead to
// use a different backend.
func NewSQLiteStorage(ctx context.Context, path string) (storage.Storage, error) {
	store, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}

	if err := store.EnsureSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}

	return store, nil
}

// NewSQLiteVectorStorage opens a sqlite-vec vector store at path, sized to the embedding
// dimensions, and prepares its schema. Point it at a different path than the metadata store to
// keep vectors in a separate database.
func NewSQLiteVectorStorage(ctx context.Context, path string, dimensions int) (storage.VectorStorage, error) {
	store, err := sqlitevec.Open(ctx, path, dimensions)
	if err != nil {
		return nil, err
	}

	return store, nil
}

// --- Strategies ---

// StrategyFactory is a deferred strategy: it declares the extensions the strategy claims (so
// the engine can reject duplicates before indexing) and builds the strategy once the engine
// supplies the shared embedder. Build may return a cleanup the engine runs after indexing
// (e.g. to release the PDF extractor); the cleanup is nil when there is nothing to release.
//
// To register a custom strategy, construct a StrategyFactory whose Build returns it.
type StrategyFactory struct {
	Extensions []string
	Build      func(embedder strategy.Embedder) (strategy.Strategy, func() error, error)
}

// NewMarkdownStrategy registers the Markdown strategy.
func NewMarkdownStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".md", ".markdown", ".mdown"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return markdown.NewMarkdownStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewPDFStrategy registers the PDF strategy. Each engine gets its own PDFium extractor, which
// the engine releases after indexing.
func NewPDFStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".pdf"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			extractor, err := pdf.NewPDFium()
			if err != nil {
				return nil, nil, err
			}

			return pdf.NewPDFStrategy(general.NewGeneralStrategy(embedder), extractor), extractor.Close, nil
		},
	}
}

// NewCodeStrategy registers the source-code strategy.
func NewCodeStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".php", ".java", ".rb", ".rs", ".c", ".h", ".cpp", ".hpp", ".cs", ".sh", ".sql"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return code.NewCodeStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewDocxStrategy registers the DOCX strategy.
func NewDocxStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".docx"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return docx.NewDocxStrategy(general.NewGeneralStrategy(embedder)), nil, nil
		},
	}
}

// NewTextStrategy registers the plain-text strategy (the general strategy used directly).
func NewTextStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".txt", ".text", ".log", ".rst", ".org", ".adoc"},
		Build: func(embedder strategy.Embedder) (strategy.Strategy, func() error, error) {
			return general.NewGeneralStrategy(embedder), nil, nil
		},
	}
}

// --- internal helpers ---

func validateConfig(config Config) error {
	switch {
	case config.Embedder == nil:
		return errors.New("embedder is required")
	case config.Storage == nil:
		return errors.New("storage is required")
	case config.VectorStorage == nil:
		return errors.New("vector storage is required")
	case len(config.Strategies) == 0:
		return errors.New("at least one strategy is required")
	}

	return validateNoDuplicateExtensions(config.Strategies)
}

// validateNoDuplicateExtensions rejects a strategy set where two strategies claim the same
// extension, which would make routing ambiguous.
func validateNoDuplicateExtensions(factories []StrategyFactory) error {
	seen := make(map[string]struct{})
	for _, factory := range factories {
		for _, ext := range factory.Extensions {
			if _, exists := seen[ext]; exists {
				return fmt.Errorf("duplicate extension %q claimed by more than one strategy", ext)
			}
			seen[ext] = struct{}{}
		}
	}

	return nil
}

// buildStrategies runs each factory with the shared embedder. It returns the strategies and a
// single release function that closes everything they opened. If a factory fails it releases
// whatever was opened so far and returns the error, so the caller just propagates it.
func buildStrategies(factories []StrategyFactory, embedder strategy.Embedder) ([]strategy.Strategy, func(), error) {
	strategies := make([]strategy.Strategy, 0, len(factories))
	var closers []func() error
	release := func() {
		for _, closer := range closers {
			closer()
		}
	}

	for _, factory := range factories {
		built, closer, err := factory.Build(embedder)
		if err != nil {
			release()
			return nil, nil, err
		}
		if closer != nil {
			closers = append(closers, closer)
		}
		strategies = append(strategies, built)
	}

	return strategies, release, nil
}

func hitChunkIDs(hits []storage.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}

func buildSearchResults(hits []storage.VectorHit, metadata []storage.ChunkMetadata) []SearchResult {
	byID := make(map[int64]storage.ChunkMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.ChunkID] = item
	}

	results := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		item, ok := byID[hit.ChunkID]
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			DocumentID: item.DocumentID,
			ChunkID:    item.ChunkID,
			Title:      item.Title,
			Text:       item.Text,
			Score:      hit.Distance,
		})
	}

	return results
}
