package semanticsearch

import (
	"context"
	"errors"

	"semantic-search/internal/embedder"
	"semantic-search/internal/ingest"
	"semantic-search/internal/pipeline"
	"semantic-search/internal/strategy"
)

type AppStore interface {
	ingest.MetadataStore
	ingest.Store
	pipeline.Store
	SearchMetadataStore
}

type IndexStore interface {
	ingest.MetadataStore
	ingest.Store
	pipeline.Store
}

// StrategyPool and Embedder are the injectable pieces of the bootstrap. The concrete
// strategies and embedder are constructed here (or by a caller) and passed into the
// pipelines.
type StrategyPool = strategy.Pool

type Embedder = pipeline.Embedder

// IndexOptions configures a single index run.
type IndexOptions struct {
	// FailFast aborts the run on the first per-document error instead of recording it
	// and continuing with the remaining documents.
	FailFast bool
	// IncludeHidden indexes hidden files and directories.
	IncludeHidden bool
	// FollowSymlinks resolves and indexes symlink targets.
	FollowSymlinks bool
}

// DefaultStrategyPool builds the default pool: Markdown only, for now.
func DefaultStrategyPool() StrategyPool {
	return strategy.NewPool(strategy.NewMarkdownStrategy())
}

// DefaultEmbedder builds the default document embedder (EmbeddingGemma-300m-qat).
func DefaultEmbedder() Embedder {
	return embedder.NewEmbeddingGemma300MQATEmbedder(embedder.DefaultBaseURL)
}

// pipelineIngest wires up and runs the ingest pipeline: it instantiates the Markdown
// strategy, builds the strategy pool with it, and calls pipeline.Ingest, which dispatches
// to each strategy's Ingest method (discover → register → fingerprint).
func pipelineIngest(ctx context.Context, store IndexStore, rootPath string, options IndexOptions) error {
	if store == nil {
		return errors.New("document store is required")
	}

	markdown := strategy.NewMarkdownStrategy()
	pool := strategy.NewPool(markdown)

	discoveryOptions := ingest.Options{
		IncludeHidden:  options.IncludeHidden,
		FollowSymlinks: options.FollowSymlinks,
	}

	return pipeline.Ingest(ctx, store, pool, rootPath, discoveryOptions, options.FailFast)
}

// pipelineProcess wires up and runs the processing pipeline: it instantiates the Markdown
// strategy and the default embedder, builds the strategy pool, and calls pipeline.Process,
// which drives documents through the strategy's read → parse → chunk steps and embeds them.
func pipelineProcess(ctx context.Context, store IndexStore, vectorStore VectorStore, options IndexOptions) error {
	if store == nil {
		return errors.New("document store is required")
	}
	if vectorStore == nil {
		return errors.New("vector store is required")
	}

	pool := strategy.NewPool(strategy.NewMarkdownStrategy())
	documentEmbedder := DefaultEmbedder()

	return pipeline.Process(ctx, store, vectorStore, pool, documentEmbedder, options.FailFast)
}

