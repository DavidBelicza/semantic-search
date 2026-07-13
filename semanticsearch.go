// Package semanticsearch is the library entry point: it composes an embedder, a metadata
// store, a vector store, and a set of strategies into an Engine that indexes a directory and
// answers meaning-based queries. Every dependency is an interface, so callers can use the
// built-in implementations (the NewXxx constructors) or supply their own.
package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/davidbelicza/semantic-search/core/embedder/client"
	"github.com/davidbelicza/semantic-search/core/embedder/model"
	"github.com/davidbelicza/semantic-search/core/search"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/storage/pgvector"
	"github.com/davidbelicza/semantic-search/core/storage/postgres"
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
// strategies. All are required except Searcher, which defaults to the built-in document searcher.
// Every dependency is an interface, so a caller can supply the built-in implementations (via the
// NewXxx constructors) or their own.
type Config struct {
	Model         strategy.EmbeddingModel
	Embedder      strategy.AiClient
	Storage       storage.Storage
	VectorStorage storage.VectorStorage
	Strategies    []StrategyFactory
	Searcher      search.Searcher
}

// Engine is a configured index/search unit. Multiple engines with different embedders, stores,
// and strategies can run independently and in parallel.
type Engine struct {
	model       strategy.EmbeddingModel
	embedder    strategy.AiClient
	store       storage.Storage
	vectorStore storage.VectorStorage
	factories   []StrategyFactory
	searcher    search.Searcher
}

// NewEngine validates the config and composes the engine. It errors on a missing dependency or
// when two strategies claim the same extension. Strategies are built per Index run (that is
// the only place they are used), so their resources live no longer than indexing needs them.
func NewEngine(config Config) (*Engine, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	searcher := config.Searcher
	if searcher == nil {
		searcher = pipeline.NewDocumentSearcher(config.Storage, config.VectorStorage, config.Model, config.Embedder)
	}

	return &Engine{
		model:       config.Model,
		embedder:    config.Embedder,
		store:       config.Storage,
		vectorStore: config.VectorStorage,
		factories:   config.Strategies,
		searcher:    searcher,
	}, nil
}

// Index runs the two pipelines: discover → register → fingerprint, then read → parse → chunk →
// embed. Re-running is incremental: unchanged files are not re-embedded. Documents whose files
// are gone are pruned unless IndexOptions.KeepMissingFiles is set. The strategies (and any
// resources they open, like the PDF extractor) are built here and released when indexing finishes.
func (e *Engine) Index(ctx context.Context, rootPath string, options IndexOptions) error {
	strategies, release, err := buildStrategies(e.factories, e.model, e.embedder)
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

	if err := pipeline.Process(ctx, e.store, e.vectorStore, pool, options.FailFast); err != nil {
		return err
	}

	if options.KeepMissingFiles {
		return nil
	}

	return pipeline.Cleanup(ctx, e.store, e.vectorStore, options.FailFast)
}

// Search embeds the query and returns the matching documents, most relevant first, each carrying
// the chunks that matched inside it. The config carries the query and its optional knobs
// (MinRelevance, MaxDocuments, MaxChunks, TaskType); passing a task type to a model that does not
// support one returns an error.
func (e *Engine) Search(ctx context.Context, config SearchConfig) ([]DocumentResult, error) {
	if strings.TrimSpace(config.Query) == "" {
		return nil, errors.New("search query is required")
	}

	return e.searcher.Search(ctx, config)
}

// IndexOptions configures an index run.
type IndexOptions struct {
	// FailFast aborts on the first per-document error instead of collecting and continuing.
	FailFast bool
	// IncludeHidden indexes hidden files and directories.
	IncludeHidden bool
	// FollowSymlinks resolves and indexes symlink targets.
	FollowSymlinks bool
	// KeepMissingFiles keeps documents whose files no longer exist on disk. By default indexing
	// removes them, along with their chunks and vectors.
	KeepMissingFiles bool
}

// SearchConfig is the whole input to a search: the query and its optional knobs. It is defined
// in core/search and re-exported here for a single-import public API.
type SearchConfig = search.SearchConfig

// SearchResult is one chunk match: the document it belongs to, the chunk id, its title
// and text, and the relevance score (0 to 1, higher is closer). It is defined in core/search
// and re-exported here for a single-import public API.
type SearchResult = search.SearchResult

// DocumentResult is one document match: its id, file name and path, relevance score, and the
// chunks that matched inside it, ranked best first. It is the type Search returns, defined in
// core/search and re-exported here for a single-import public API.
type DocumentResult = search.DocumentResult

// --- Model ---

// Model-specific query task types for Search. Any string is accepted; these are just the tasks
// each model documents.
var (
	TaskGemma = model.GemmaTasks
	TaskNomic = model.NomicTasks
)

// PredefinedModel selects one of the built-in embedding models by name. Each one bundles the
// model's id, vector size, and the prompt templates it needs, so callers do not hand-write
// templates. For a model that is not listed, implement strategy.EmbeddingModel yourself and inject it.
type PredefinedModel string

// Predefined embedding models. Each constant bundles a model id, its native vector size, and the
// prompt templates the model requires. The dimension is encoded in the constant name so it is
// visible at the call site (Gemma keeps its parameter-based name).
const (
	// Gemma300mQAT is EmbeddingGemma (text-embedding-embeddinggemma-300m-qat, 768 dimensions).
	Gemma300mQAT PredefinedModel = "gemma-300m-qat"
	// Nomic768 is Nomic Embed Text v1.5 (768 dimensions).
	Nomic768 PredefinedModel = "nomic-v1.5"
	// E5Large1024 is Multilingual E5 large (1024 dimensions).
	E5Large1024 PredefinedModel = "e5-large"
	// BGELarge1024 is BGE large en v1.5 (1024 dimensions).
	BGELarge1024 PredefinedModel = "bge-large-en-v1.5"
	// Qwen30_6B1024 is Qwen3 Embedding 0.6B (1024 dimensions).
	Qwen30_6B1024 PredefinedModel = "qwen3-0.6b"
	// MxbaiLarge1024 is mxbai embed large v1 (1024 dimensions).
	MxbaiLarge1024 PredefinedModel = "mxbai-large-v1"
)

// NewModel builds the model knowledge (id, dimensions, prompt templates) for a predefined
// model. Any value that is not a predefined constant is treated as a raw model id and returned
// as a template-free GeneralModel with the given dimensions. The dimensions argument is
// optional and ignored for predefined models that have a fixed vector size; supply it only for
// an unlisted model id.
func NewModel(predefined PredefinedModel, dimensions ...int) strategy.EmbeddingModel {
	switch predefined {
	case Gemma300mQAT:
		return model.GemmaModel{}
	case Nomic768:
		return model.NomicModel{}
	case E5Large1024:
		return model.E5LargeModel{}
	case BGELarge1024:
		return model.BGELargeModel{}
	case Qwen30_6B1024:
		return model.Qwen3SmallModel{}
	case MxbaiLarge1024:
		return model.MxbaiLargeModel{}
	}

	dim := 0
	if len(dimensions) > 0 {
		dim = dimensions[0]
	}

	return model.NewGeneralModel(string(predefined), dim)
}

// NewGeneralModel builds a template-free model with the given model id and vector size, for any
// OpenAI-standard model that needs no prompt templates. Switching models or vector sizes is
// then just a different call, with no new type to implement.
func NewGeneralModel(name string, dimensions int) strategy.EmbeddingModel {
	return model.NewGeneralModel(name, dimensions)
}

// --- Embedder ---

// Standard identifies the wire protocol an AI embedder speaks. Most embedding servers are
// OpenAI-compatible; other standards can be added later without changing callers.
type Standard string

// StandardOpenAI is the OpenAI-compatible /v1/embeddings protocol (LM Studio, Ollama, and
// most local servers).
const StandardOpenAI Standard = "openai"

// AiEmbedderConfig configures the transport client: which protocol to speak and the endpoint,
// auth, and timeout to use. The model id and vector size come from the injected Model, not
// from here.
type AiEmbedderConfig struct {
	Standard Standard
	BaseURL  string
	// APIKey is optional. When set it is sent as an "Authorization: Bearer <APIKey>" header
	// for hosted endpoints; leave it empty for local servers that need no authentication.
	APIKey string
	// Timeout is optional per-request timeout for embedding calls. Zero uses the default.
	Timeout time.Duration
}

// NewAiEmbedder builds the transport client for the given standard, configured to send the
// model's id and validate its vector size. It returns nil for an unknown standard or a nil
// model; NewEngine rejects a nil embedder.
func NewAiEmbedder(config AiEmbedderConfig, model strategy.EmbeddingModel) strategy.AiClient {
	if config.Standard != StandardOpenAI || model == nil {
		return nil
	}

	c := client.NewOpenAIClient(config.BaseURL, model.Name())
	c.Dimensions = model.Dimensions()
	c.APIKey = config.APIKey
	if config.Timeout > 0 {
		c.HTTPClient = &http.Client{Timeout: config.Timeout}
	}

	return c
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

// NewPostgresStorage opens a PostgreSQL metadata store at dsn (e.g.
// "postgres://user:pass@host:5432/db?sslmode=disable") and prepares its schema. It uses the
// pure-Go pgx driver, so a Postgres-only build needs no cgo.
func NewPostgresStorage(ctx context.Context, dsn string) (storage.Storage, error) {
	store, err := postgres.Open(dsn)
	if err != nil {
		return nil, err
	}

	if err := store.EnsureSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}

	return store, nil
}

// PostgresVectorIndex selects how the pgvector store searches.
type PostgresVectorIndex string

const (
	// PostgresKNN is exact brute-force k-nearest-neighbor search (a sequential scan, 100%
	// recall). Best below a few hundred thousand vectors.
	PostgresKNN PostgresVectorIndex = "knn"
	// PostgresHNSW is approximate nearest-neighbor search backed by an HNSW index
	// (sub-linear, trades some recall for speed). Best at large scale.
	PostgresHNSW PostgresVectorIndex = "hnsw"
)

// NewPostgresVectorStorage opens a pgvector vector store at dsn, sized to the embedding
// dimensions, and prepares its schema. The server must have the pgvector extension available.
// The index selects exact (PostgresKNN) or approximate (PostgresHNSW) search. Point it at a
// different dsn than the metadata store to keep vectors in a separate database.
func NewPostgresVectorStorage(ctx context.Context, dsn string, dimensions int, index PostgresVectorIndex) (storage.VectorStorage, error) {
	store, err := pgvector.Open(ctx, dsn, dimensions, index == PostgresHNSW)
	if err != nil {
		return nil, err
	}

	return store, nil
}

// --- Strategies ---

// StrategyFactory is a deferred strategy: it declares the extensions the strategy claims (so
// the engine can reject duplicates before indexing) and builds the strategy once the engine
// supplies the shared model and embedder. Build may return a cleanup the engine runs after
// indexing (e.g. to release the PDF extractor); the cleanup is nil when there is nothing to
// release.
//
// To register a custom strategy, construct a StrategyFactory whose Build returns it.
type StrategyFactory struct {
	Extensions []string
	Build      func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error)
}

// NewMarkdownStrategy registers the Markdown strategy.
func NewMarkdownStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".md", ".markdown", ".mdown"},
		Build: func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error) {
			return markdown.NewMarkdownStrategy(general.NewGeneralStrategy(model, embedder)), nil, nil
		},
	}
}

// NewPDFStrategy registers the PDF strategy. Each engine gets its own PDFium extractor, which
// the engine releases after indexing.
func NewPDFStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".pdf"},
		Build: func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error) {
			extractor, err := pdf.NewPDFium()
			if err != nil {
				return nil, nil, err
			}

			return pdf.NewPDFStrategy(general.NewGeneralStrategy(model, embedder), extractor), extractor.Close, nil
		},
	}
}

// NewCodeStrategy registers the source-code strategy.
func NewCodeStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".php", ".java", ".rb", ".rs", ".c", ".h", ".cpp", ".hpp", ".cs", ".sh", ".sql"},
		Build: func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error) {
			return code.NewCodeStrategy(general.NewGeneralStrategy(model, embedder)), nil, nil
		},
	}
}

// NewDocxStrategy registers the DOCX strategy.
func NewDocxStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".docx"},
		Build: func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error) {
			return docx.NewDocxStrategy(general.NewGeneralStrategy(model, embedder)), nil, nil
		},
	}
}

// NewTextStrategy registers the plain-text strategy (the general strategy used directly).
func NewTextStrategy() StrategyFactory {
	return StrategyFactory{
		Extensions: []string{".txt", ".text", ".log", ".rst", ".org", ".adoc"},
		Build: func(model strategy.EmbeddingModel, embedder strategy.AiClient) (strategy.Strategy, func() error, error) {
			return general.NewGeneralStrategy(model, embedder), nil, nil
		},
	}
}

// --- internal helpers ---

func validateConfig(config Config) error {
	switch {
	case config.Model == nil:
		return errors.New("model is required")
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
func buildStrategies(factories []StrategyFactory, model strategy.EmbeddingModel, embedder strategy.AiClient) ([]strategy.Strategy, func(), error) {
	strategies := make([]strategy.Strategy, 0, len(factories))
	var closers []func() error
	release := func() {
		for _, closer := range closers {
			closer()
		}
	}

	for _, factory := range factories {
		built, closer, err := factory.Build(model, embedder)
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
