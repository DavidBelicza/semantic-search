package pipeline

import (
	"context"

	"semantic-search/internal/ingest"
	"semantic-search/internal/strategy"
)

// Ingest runs the file-level ingest pipeline for every strategy in the pool: discovery →
// registration → fingerprinting. The per-step work lives in the strategies' methods; this
// function orchestrates them by calling those methods in order.
func Ingest(ctx context.Context, store strategy.IngestStore, pool strategy.Pool, rootPath string, options ingest.Options, failFast bool) error {
	for _, fileStrategy := range pool.Strategies() {
		files, err := fileStrategy.Discovery(rootPath, options)
		if err != nil {
			return err
		}

		if err := fileStrategy.Registration(ctx, store, files); err != nil {
			return err
		}

		if err := fileStrategy.Fingerprinting(ctx, store, failFast); err != nil {
			return err
		}
	}

	return nil
}
