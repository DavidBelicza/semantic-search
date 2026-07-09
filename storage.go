package semanticsearch

import (
	"context"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/storage/sqlite"
	"github.com/davidbelicza/semantic-search/core/storage/sqlitevec"
)

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
