// Package pipeline is the flow: the movement between files. Pipelines are functions that
// walk and iterate files, read their bytes, call the strategy's per-file steps, and own
// everything between files — database writes and the decisions that advance or stop the
// flow. They contain no per-file processing logic; that belongs to the strategy.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

const (
	documentUpsertBatchSize = 500
	fingerprintBatchSize    = 100
)

// Options controls how the index pipeline walks the tree.
type Options struct {
	IncludeHidden  bool
	FollowSymlinks bool
}

// IndexStore is the metadata surface the index pipeline needs — a subset of storage.Storage,
// which any injected store (sqlite, Postgres, …) satisfies.
type IndexStore interface {
	UpsertDocuments(ctx context.Context, files []storage.FileMetadata) error
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error)
	UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error
	UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error
}

// Index is the "all files in one round" pipeline: walk the tree, register the documents a
// strategy claims, then fingerprint the indexed ones to detect content changes. progress may
// be nil.
func Index(ctx context.Context, store IndexStore, pool strategy.Pool, rootPath string, options Options, failFast bool, progress *ProgressTracker) error {
	progress.Start(PhaseScanning, 0)

	files, err := discover(pool, rootPath, options)
	if err != nil {
		return err
	}

	if err := upsertInBatches(ctx, store, files); err != nil {
		return err
	}

	progress.Start(PhaseIndexing, len(files))

	return fingerprint(ctx, store, pool, failFast, progress)
}

func discover(pool strategy.Pool, root string, options Options) ([]storage.FileMetadata, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootAbs = filepath.Clean(rootAbs)

	var files []storage.FileMetadata
	err = filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return directoryAction(rootAbs, path, entry, options)
		}

		metadata, ok, err := fileMetadata(pool, path, entry, options)
		if err != nil {
			return err
		}
		if ok {
			files = append(files, metadata)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func directoryAction(rootAbs string, path string, entry fs.DirEntry, options Options) error {
	if path == rootAbs {
		return nil
	}
	if !options.IncludeHidden && isHidden(entry.Name()) {
		return filepath.SkipDir
	}

	return nil
}

// fileMetadata asks the pool which strategy claims the file and lets that strategy build
// its metadata. Whether a file is ingestable is the strategy's decision, not the
// pipeline's.
func fileMetadata(pool strategy.Pool, path string, entry fs.DirEntry, options Options) (storage.FileMetadata, bool, error) {
	if !options.IncludeHidden && isHidden(entry.Name()) {
		return storage.FileMetadata{}, false, nil
	}

	fileStrategy, ok := pool.For(path)
	if !ok {
		return storage.FileMetadata{}, false, nil
	}

	info, err := fileInfo(path, entry, options)
	if err != nil {
		return storage.FileMetadata{}, false, err
	}
	if !info.Mode().IsRegular() {
		return storage.FileMetadata{}, false, nil
	}

	metadata, err := fileStrategy.CreateMetadata(strategy.FileRef{Path: path, Info: info})
	if err != nil {
		return storage.FileMetadata{}, false, err
	}

	return metadata, true, nil
}

func fileInfo(path string, entry fs.DirEntry, options Options) (fs.FileInfo, error) {
	if entry.Type()&fs.ModeSymlink == 0 {
		return entry.Info()
	}
	if !options.FollowSymlinks {
		return entry.Info()
	}

	return os.Stat(path)
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func upsertInBatches(ctx context.Context, store IndexStore, files []storage.FileMetadata) error {
	for start := 0; start < len(files); start += documentUpsertBatchSize {
		end := start + documentUpsertBatchSize
		if end > len(files) {
			end = len(files)
		}
		if err := store.UpsertDocuments(ctx, files[start:end]); err != nil {
			return err
		}
	}

	return nil
}

// fingerprint walks the indexed documents, hashes changed ones via their strategy, and
// advances their status. The status decisions (advance to scanned, restore to embedded)
// are the pipeline's; the hashing is the strategy's.
func fingerprint(ctx context.Context, store IndexStore, pool strategy.Pool, failFast bool, progress *ProgressTracker) error {
	var errs []error
	var afterID int64

	for {
		documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusIndexed, afterID, fingerprintBatchSize)
		if err != nil {
			return err
		}
		if len(documents) == 0 {
			return errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			err := fingerprintDocument(ctx, store, pool, document, progress)
			if err == nil {
				continue
			}
			if failFast {
				return err
			}
			errs = append(errs, err)
		}
	}
}

func fingerprintDocument(ctx context.Context, store IndexStore, pool strategy.Pool, document storage.Document, progress *ProgressTracker) error {
	if metadataMatchesCheckpoint(document) {
		return markCheckpoint(ctx, store, document, storage.DocumentStatusScanned)
	}

	fileStrategy, ok := pool.For(document.AbsolutePath)
	if !ok {
		return fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	content, err := os.ReadFile(document.AbsolutePath)
	if err != nil {
		return fmt.Errorf("read file %q: %w", document.AbsolutePath, err)
	}
	hash := fileStrategy.Fingerprint(content)

	// Content matches what was already embedded (e.g. the file was only touched):
	// restore embedded and skip re-processing.
	if document.EmbeddedContentHash != "" && document.EmbeddedContentHash == hash {
		return skipEmbedded(ctx, store, document, progress)
	}
	if document.HasHash && document.ContentHash == hash {
		return markCheckpoint(ctx, store, document, storage.DocumentStatusScanned)
	}

	if err := store.UpdateDocumentContentHashAndStatus(ctx, document.FileID, hash, storage.DocumentStatusScanned); err != nil {
		return fmt.Errorf("update scanned document %q: %w", document.AbsolutePath, err)
	}

	return nil
}

func skipEmbedded(ctx context.Context, store IndexStore, document storage.Document, progress *ProgressTracker) error {
	if err := markCheckpoint(ctx, store, document, storage.DocumentStatusEmbedded); err != nil {
		return err
	}
	progress.Advance()

	return nil
}

func metadataMatchesCheckpoint(document storage.Document) bool {
	return document.HasHash && document.HasScannedMetadata &&
		document.FileSize == document.ScannedFileSize &&
		document.ModifiedAtNS == document.ScannedModifiedAtNS
}

func markCheckpoint(ctx context.Context, store IndexStore, document storage.Document, status string) error {
	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, document.FileID, status); err != nil {
		return fmt.Errorf("mark document %q as %s: %w", document.AbsolutePath, status, err)
	}

	return nil
}
