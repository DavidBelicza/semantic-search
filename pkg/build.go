package semanticsearch

import (
	"context"

	"semantic-search/internal/embedder"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/storage/sqlitevec"
	"semantic-search/internal/strategy"
)

// dependencies is the fully instantiated object graph the pipelines need.
type dependencies struct {
	store       *storage.Store
	vectorStore *sqlitevec.Store
	pool        strategy.Pool
}

// build is the single place that instantiates the whole dependency graph: it opens the
// stores and composes the strategy pool (embedder → GeneralStrategy → MarkdownStrategy →
// Pool). The embedder is built here and injected into the strategy, since embedding is a
// per-file operation the strategy owns.
func build(ctx context.Context, dbPath string) (dependencies, error) {
	store, vectorStore, err := openStores(ctx, dbPath)
	if err != nil {
		return dependencies{}, err
	}

	documentEmbedder := embedder.NewEmbeddingGemma300MQATEmbedder(embedder.DefaultBaseURL)
	general := strategy.NewGeneralStrategy(documentEmbedder)
	markdown := strategy.NewMarkdownStrategy(general)
	pool := strategy.NewPool(markdown)

	return dependencies{store: store, vectorStore: vectorStore, pool: pool}, nil
}

func (d dependencies) close() {
	if d.vectorStore != nil {
		d.vectorStore.Close()
	}
	if d.store != nil {
		d.store.Close()
	}
}

// openStores opens and prepares the SQLite metadata store and the sqlite-vec vector store,
// both backed by the same database file.
func openStores(ctx context.Context, dbPath string) (*storage.Store, *sqlitevec.Store, error) {
	store, err := storage.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	if err := store.EnsureSchema(ctx); err != nil {
		store.Close()
		return nil, nil, err
	}

	vectorStore, err := sqlitevec.Open(ctx, dbPath, embedder.DefaultDimensions)
	if err != nil {
		store.Close()
		return nil, nil, err
	}

	return store, vectorStore, nil
}
