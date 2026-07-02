// Package strategy defines the per-format processing contract. A Strategy owns the
// format-specific steps (read, parse, chunk), declares the file extensions it handles,
// and runs the file-level ingest step (discover → register → fingerprint) via Ingest.
// Chunk-level orchestration (reconciliation, embedding, storage) still lives in the
// pipeline package; embedding is injected there separately.
package strategy

import (
	"context"
	"path/filepath"
	"strings"

	"semantic-search/internal/ingest"
	storage "semantic-search/internal/storage/sqlite"
)

// IngestStore is the store surface a strategy's Ingest step needs: registering
// discovered documents and fingerprinting indexed ones.
type IngestStore interface {
	ingest.MetadataStore
	ingest.Store
}

// Strategy is the format-specific half of the pipeline. Each supported file format is a
// concrete implementation. Shared behaviour between implementations is expressed as
// package-level helper functions the implementations call, not via inheritance.
type Strategy interface {
	// Extensions returns the lower-case file extensions (with leading dot) this strategy
	// handles, e.g. []string{".md", ".markdown"}.
	Extensions() []string
	// Discovery walks rootPath and returns the files it finds.
	Discovery(rootPath string, options ingest.Options) ([]storage.FileMetadata, error)
	// Registration records the files this strategy supports as documents.
	Registration(ctx context.Context, store ingest.MetadataStore, files []storage.FileMetadata) error
	// Fingerprinting hashes indexed documents to detect content changes.
	Fingerprinting(ctx context.Context, store ingest.Store, failFast bool) error
	Read(ctx context.Context, doc storage.Document) (string, error)
	Parse(ctx context.Context, text string) (string, error)
	Chunk(ctx context.Context, doc storage.Document, text string) ([]storage.Chunk, error)
}

// Pool holds the configured strategies and resolves one for a given path.
type Pool struct {
	strategies []Strategy
}

func NewPool(strategies ...Strategy) Pool {
	return Pool{strategies: strategies}
}

// Strategies returns the pool's strategies in order.
func (p Pool) Strategies() []Strategy {
	return p.strategies
}

// Find returns the first strategy that handles the given path's extension.
func (p Pool) Find(path string) (Strategy, bool) {
	extension := strings.ToLower(filepath.Ext(path))
	for _, strategy := range p.strategies {
		if extensionsContain(strategy.Extensions(), extension) {
			return strategy, true
		}
	}

	return nil, false
}

// Supports reports whether any strategy handles the given path.
func (p Pool) Supports(path string) bool {
	_, ok := p.Find(path)
	return ok
}

func extensionsContain(extensions []string, extension string) bool {
	for _, supported := range extensions {
		if strings.ToLower(supported) == extension {
			return true
		}
	}

	return false
}
