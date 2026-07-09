package semanticsearch

import (
	"context"
	"path/filepath"
	"testing"
)

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
