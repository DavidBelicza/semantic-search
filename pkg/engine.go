package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/davidbelicza/semantic-search/core/embedder"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/pipeline"
)

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

