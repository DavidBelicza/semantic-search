package semanticsearch

import (
	"context"
	"errors"
)

// Index runs the full indexing flow for rootPath. It is the single public entry point for
// indexing: it runs the ingest pipeline (discover → register → fingerprint) and then the
// processing pipeline (read → parse → chunk → embed).
func Index(ctx context.Context, store IndexStore, vectorStore VectorStore, rootPath string, options IndexOptions) error {
	if store == nil {
		return errors.New("document store is required")
	}
	if vectorStore == nil {
		return errors.New("vector store is required")
	}

	if err := pipelineIngest(ctx, store, rootPath, options); err != nil {
		return err
	}

	return pipelineProcess(ctx, store, vectorStore, options)
}
