package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/davidbelicza/semantic-search/core/storage"
)

const cleanupBatchSize = 500

// CleanupStore is the metadata surface the cleanup pipeline needs — a subset of storage.Storage.
type CleanupStore interface {
	DocumentsFromID(ctx context.Context, fromID int64, limit int) ([]storage.Document, error)
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error)
	DeleteDocument(ctx context.Context, documentID int64) error
}

// CleanupVectorStore is the vector write surface the cleanup pipeline needs — a subset of
// storage.VectorStorage.
type CleanupVectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
}

// Cleanup pages through every stored document and removes the ones whose file is confirmed missing,
// along with their chunks and vectors. Run it after indexing, so a moved file (its path just
// refreshed by the walk) is not mistaken for a deleted one — which holds where identity survives
// a move.
func Cleanup(ctx context.Context, store CleanupStore, vectorStore CleanupVectorStore, failFast bool, progress *ProgressTracker) error {
	var errs []error
	var afterID int64

	progress.Start(PhaseCleanup, 0)

	for {
		documents, err := store.DocumentsFromID(ctx, afterID, cleanupBatchSize)
		if err != nil {
			return err
		}
		if len(documents) == 0 {
			return errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			err := cleanupDocument(ctx, store, vectorStore, document)
			progress.Advance()
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

// cleanupDocument deletes the document only if its file is confirmed missing; a present file or an
// ambiguous stat error is left untouched. Vectors go before the metadata so a crash can't strand a
// document whose vectors are already gone.
func cleanupDocument(ctx context.Context, store CleanupStore, vectorStore CleanupVectorStore, document storage.Document) error {
	_, err := os.Stat(document.AbsolutePath)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat document %q: %w", document.AbsolutePath, err)
	}

	chunks, err := store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return fmt.Errorf("chunks for %q: %w", document.AbsolutePath, err)
	}

	if err := vectorStore.Delete(ctx, chunkIDs(chunks)); err != nil {
		return fmt.Errorf("delete vectors for %q: %w", document.AbsolutePath, err)
	}

	if err := store.DeleteDocument(ctx, document.ID); err != nil {
		return fmt.Errorf("delete document %q: %w", document.AbsolutePath, err)
	}

	return nil
}

// chunkIDs pulls the ids out of the chunks.
func chunkIDs(chunks []storage.Chunk) []int64 {
	ids := make([]int64, len(chunks))
	for i, chunk := range chunks {
		ids[i] = chunk.ID
	}

	return ids
}
