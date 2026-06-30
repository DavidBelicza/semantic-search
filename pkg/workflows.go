package semanticsearch

import (
	"context"
	"errors"

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

func DefaultStrategyPool() StrategyPool {
	return strategy.DefaultPool()
}

func Index(ctx context.Context, store IndexStore, vectorStore VectorStore, strategyPool StrategyPool, rootPath string) error {
	if store == nil {
		return errors.New("document store is required")
	}
	if vectorStore == nil {
		return errors.New("vector store is required")
	}

	if err := indexer.IndexPath(ctx, store, rootPath, strategyPool); err != nil {
		return err
	}

	if _, err := scanner.ScanIndexedDocuments(ctx, store); err != nil {
		return err
	}

	if _, err := strategy.ProcessScannedDocuments(ctx, store, vectorStore, strategyPool); err != nil {
		return err
	}

	_, err := strategy.ProcessChunkedDocuments(ctx, store, vectorStore, strategyPool)
	return err
}

func Scan(ctx context.Context, store scanner.Store) error {
	if store == nil {
		return errors.New("document store is required")
	}

	_, err := scanner.ScanIndexedDocuments(ctx, store)
	return err
}
