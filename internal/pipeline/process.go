package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

const processBatchSize = 1

// ProcessStore is the metadata surface the process pipeline needs — a subset of
// storage.Storage, which any injected store satisfies.
type ProcessStore interface {
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error)
	ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error)
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error)
	UpdateDocumentStatus(ctx context.Context, fileID string, status string) error
	MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error
}

// ProcessVectorStore is the vector write surface the process pipeline needs — a subset of
// storage.VectorStorage.
type ProcessVectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error
}

// Process is the "file by file" pipeline: for each scanned document it reads the bytes,
// runs the strategy's parse → chunk → embed steps, and owns the reconciliation, status
// transitions, and vector writes between them. Documents left in the chunked state (a
// previous run that embedded partially) are then embedded.
func Process(ctx context.Context, store ProcessStore, vectorStore ProcessVectorStore, pool strategy.Pool, failFast bool) error {
	run := processor{store: store, vectorStore: vectorStore, pool: pool}

	if err := run.byStatus(ctx, storage.DocumentStatusScanned, failFast, run.processScanned); err != nil {
		return err
	}

	return run.byStatus(ctx, storage.DocumentStatusChunked, failFast, run.embedChunked)
}

// processor bundles the dependencies so the per-document helpers stay readable; it is a
// private detail — the pipeline's public surface is the Process function.
type processor struct {
	store       ProcessStore
	vectorStore ProcessVectorStore
	pool        strategy.Pool
}

type documentHandler func(ctx context.Context, document storage.Document) error

func (p processor) byStatus(ctx context.Context, status string, failFast bool, handle documentHandler) error {
	var errs []error
	var afterID int64

	for {
		documents, err := p.store.DocumentsByStatus(ctx, status, afterID, processBatchSize)
		if err != nil {
			return err
		}
		if len(documents) == 0 {
			return errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			err := handle(ctx, document)
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

func (p processor) processScanned(ctx context.Context, document storage.Document) error {
	fileStrategy, ok := p.pool.For(document.AbsolutePath)
	if !ok {
		return fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := readParseChunk(fileStrategy, document)
	if err != nil {
		return fmt.Errorf("process document %q: %w", document.AbsolutePath, err)
	}

	existingChunks, err := p.store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return fmt.Errorf("load existing chunks for %q: %w", document.AbsolutePath, err)
	}

	plan := storage.ReconcileChunks(existingChunks, chunks)
	insertedChunks, err := p.store.ApplyDocumentChunkReconcile(ctx, document.ID, plan)
	if err != nil {
		return fmt.Errorf("reconcile chunks for %q: %w", document.AbsolutePath, err)
	}

	// SQLite is the source of truth: the chunk rows are committed before the vector store
	// is mutated, so a crash cannot leave chunks pointing at deleted vectors.
	if err := p.vectorStore.Delete(ctx, plan.RemoveIDs); err != nil {
		return fmt.Errorf("delete old vectors for %q: %w", document.AbsolutePath, err)
	}
	if err := p.store.UpdateDocumentStatus(ctx, document.FileID, storage.DocumentStatusChunked); err != nil {
		return fmt.Errorf("mark document chunked %q: %w", document.AbsolutePath, err)
	}

	chunksToEmbed, err := p.chunksForEmbedding(ctx, document, insertedChunks, existingChunks)
	if err != nil {
		return err
	}
	if err := p.embed(ctx, fileStrategy, document, chunksToEmbed); err != nil {
		return err
	}

	return p.markEmbedded(ctx, document)
}

func (p processor) embedChunked(ctx context.Context, document storage.Document) error {
	fileStrategy, ok := p.pool.For(document.AbsolutePath)
	if !ok {
		return fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := p.store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}
	if err := p.embed(ctx, fileStrategy, document, chunks); err != nil {
		return err
	}

	return p.markEmbedded(ctx, document)
}

// readParseChunk runs the strategy's per-file steps in sequence. The pipeline reads the
// bytes and hands them in; the strategy does the processing.
func readParseChunk(fileStrategy strategy.Strategy, document storage.Document) ([]storage.Chunk, error) {
	content, err := os.ReadFile(document.AbsolutePath)
	if err != nil {
		return nil, err
	}

	parsed, err := fileStrategy.Parse(content)
	if err != nil {
		return nil, err
	}

	return fileStrategy.Chunk(document, parsed)
}

// chunksForEmbedding selects which chunks still need vectors. An already-embedded
// document keeps valid vectors for its unchanged chunks, so only newly inserted chunks
// need embedding; a never-embedded document embeds all of its current chunks.
func (p processor) chunksForEmbedding(ctx context.Context, document storage.Document, inserted []storage.Chunk, existing []storage.Chunk) ([]storage.Chunk, error) {
	if document.EmbeddedContentHash != "" {
		return inserted, nil
	}
	if len(existing) == 0 {
		return inserted, nil
	}

	chunks, err := p.store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return nil, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}

	return chunks, nil
}

func (p processor) embed(ctx context.Context, fileStrategy strategy.Strategy, document storage.Document, chunks []storage.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	vectors, err := fileStrategy.Embed(ctx, chunks)
	if err != nil {
		return fmt.Errorf("embed chunks for %q: %w", document.AbsolutePath, err)
	}
	if len(vectors) != len(chunks) {
		return fmt.Errorf("embedding count mismatch for %q: want %d, got %d", document.AbsolutePath, len(chunks), len(vectors))
	}

	dimensions := len(vectors[0])
	embeddings := make([]storage.ChunkEmbedding, len(chunks))
	for i, chunk := range chunks {
		if len(vectors[i]) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for %q chunk %d", document.AbsolutePath, chunk.ChunkIndex)
		}
		embeddings[i] = storage.ChunkEmbedding{ChunkID: chunk.ID, Vector: vectors[i]}
	}

	if err := p.vectorStore.Replace(ctx, embeddings); err != nil {
		return fmt.Errorf("store vectors for %q: %w", document.AbsolutePath, err)
	}

	return nil
}

func (p processor) markEmbedded(ctx context.Context, document storage.Document) error {
	if err := p.store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}

	return nil
}
