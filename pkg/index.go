package semanticsearch

import (
	"context"

	"github.com/davidbelicza/semantic-search/internal/pipeline"
)

// IndexOptions configures an index run.
type IndexOptions struct {
	// FailFast aborts on the first per-document error instead of collecting and continuing.
	FailFast bool
	// IncludeHidden indexes hidden files and directories.
	IncludeHidden bool
	// FollowSymlinks resolves and indexes symlink targets.
	FollowSymlinks bool
}

// Index is the bootstrapper entry point: it instantiates the whole dependency graph and
// then runs the two pipelines — the index pipeline (discover → register → fingerprint)
// and the process pipeline (read → parse → chunk → embed).
func Index(ctx context.Context, dbPath string, rootPath string, options IndexOptions) error {
	deps, err := build(ctx, dbPath)
	if err != nil {
		return err
	}
	defer deps.close()

	if err := indexPipeline(ctx, deps, rootPath, options); err != nil {
		return err
	}

	return processPipeline(ctx, deps, options)
}

// indexPipeline receives the already-built dependencies and runs the index pipeline.
func indexPipeline(ctx context.Context, deps dependencies, rootPath string, options IndexOptions) error {
	walkOptions := pipeline.Options{
		IncludeHidden:  options.IncludeHidden,
		FollowSymlinks: options.FollowSymlinks,
	}

	return pipeline.Index(ctx, deps.store, deps.pool, rootPath, walkOptions, options.FailFast)
}

// processPipeline receives the already-built dependencies and runs the process pipeline.
func processPipeline(ctx context.Context, deps dependencies, options IndexOptions) error {
	return pipeline.Process(ctx, deps.store, deps.vectorStore, deps.pool, options.FailFast)
}
