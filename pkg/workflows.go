package semanticsearch

import (
	"context"
	"errors"

	"semantic-search/internal/crawler"
	"semantic-search/internal/indexer"
	"semantic-search/internal/scanner"
	"semantic-search/internal/strategy"
)

type AppStore interface {
	indexer.MetadataStore
	scanner.Store
	strategy.Store
}

type IndexStore interface {
	indexer.MetadataStore
	scanner.Store
	strategy.Store
}

type VectorStore = strategy.VectorStore
type StrategyPool = strategy.Pool

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

func DefaultStrategyPool() StrategyPool {
	return strategy.DefaultPool()
}

func Index(ctx context.Context, store IndexStore, vectorStore VectorStore, strategyPool StrategyPool, rootPath string, options IndexOptions) error {
	if store == nil {
		return errors.New("document store is required")
	}
	if vectorStore == nil {
		return errors.New("vector store is required")
	}

	crawlerOptions := crawler.Options{
		IncludeHidden:  options.IncludeHidden,
		FollowSymlinks: options.FollowSymlinks,
	}
	if err := indexer.IndexPath(ctx, store, rootPath, strategyPool, crawlerOptions); err != nil {
		return err
	}

	if _, err := scanner.ScanIndexedDocuments(ctx, store, options.FailFast); err != nil {
		return err
	}

	if _, err := strategy.ProcessScannedDocuments(ctx, store, vectorStore, strategyPool, options.FailFast); err != nil {
		return err
	}

	_, err := strategy.ProcessChunkedDocuments(ctx, store, vectorStore, strategyPool, options.FailFast)
	return err
}

func Scan(ctx context.Context, store scanner.Store, failFast bool) error {
	if store == nil {
		return errors.New("document store is required")
	}

	_, err := scanner.ScanIndexedDocuments(ctx, store, failFast)
	return err
}

// Rebuild re-embeds every embedded document's chunks and replaces their vectors,
// rebuilding the LanceDB index from SQLite. Per-document failures are collected and
// reported rather than aborting the run.
func Rebuild(ctx context.Context, store IndexStore, vectorStore VectorStore, strategyPool StrategyPool) error {
	if store == nil {
		return errors.New("document store is required")
	}
	if vectorStore == nil {
		return errors.New("vector store is required")
	}

	_, err := strategy.RebuildVectors(ctx, store, vectorStore, strategyPool, false)
	return err
}
