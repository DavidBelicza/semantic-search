package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

const (
	processDocumentBatchSize = 100
	// defaultEmbedBatchSize is used when the caller does not set a positive embed batch size.
	defaultEmbedBatchSize = 50
)

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
//
// Chunks do not go to the embedding server one document at a time: they are buffered across
// documents and sent in batches (see embedBuffer). A document is marked embedded when the
// batch carrying its chunks is stored, so each pass buffers, then embeds the remainder.
func Process(ctx context.Context, store ProcessStore, vectorStore ProcessVectorStore, pool strategy.Pool, failFast bool, embedBatchSize int, progress *ProgressTracker) error {
	if embedBatchSize <= 0 {
		embedBatchSize = defaultEmbedBatchSize
	}
	run := processor{store: store, vectorStore: vectorStore, pool: pool, progress: progress, failFast: failFast, embedBatchSize: embedBatchSize, buffer: newEmbedBuffer(embedBatchSize)}

	var errs []error
	steps := []func(context.Context) error{
		func(ctx context.Context) error {
			return run.forEachInStatus(ctx, storage.DocumentStatusScanned, run.processScanned)
		},
		run.embedRemaining,
		func(ctx context.Context) error {
			return run.forEachInStatus(ctx, storage.DocumentStatusChunked, run.embedChunked)
		},
		run.embedRemaining,
	}
	for _, step := range steps {
		err := step(ctx)
		if err == nil {
			continue
		}
		if failFast {
			return err
		}
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// processor bundles the dependencies so the per-document helpers stay readable; it is a
// private detail — the pipeline's public surface is the Process function.
type processor struct {
	store          ProcessStore
	vectorStore    ProcessVectorStore
	pool           strategy.Pool
	progress       *ProgressTracker
	failFast       bool
	embedBatchSize int
	buffer         *embedBuffer
}

type documentHandler func(ctx context.Context, document storage.Document) error

func (p processor) forEachInStatus(ctx context.Context, status string, handle documentHandler) error {
	var errs []error
	var afterID int64

	for {
		documents, err := p.store.DocumentsByStatus(ctx, status, afterID, processDocumentBatchSize)
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
			if p.failFast {
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

	return p.processBatch(ctx, p.buffer.Enqueue(embedItem{strategy: fileStrategy, document: document, chunks: chunksToEmbed}))
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

	return p.processBatch(ctx, p.buffer.Enqueue(embedItem{strategy: fileStrategy, document: document, chunks: chunks}))
}

// embedRemaining embeds whatever is still buffered, for the end of a pass.
func (p processor) embedRemaining(ctx context.Context) error {
	return p.processBatch(ctx, p.buffer.Flush())
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

// processBatch embeds a flushed batch's chunks in one run of requests, stores the vectors in a
// single write, then marks the batch's documents. The batch succeeds or fails as a unit: a
// failure leaves its documents in the chunked state for the next run to retry.
func (p processor) processBatch(ctx context.Context, items []embedItem) error {
	if err := p.embedAndStoreVectors(ctx, items); err != nil {
		return err
	}

	for _, item := range items {
		if err := p.markEmbedded(ctx, item.document); err != nil {
			return err
		}
	}

	return nil
}

// embedAndStoreVectors embeds every item's chunks in one run of requests and writes the vectors once.
// All strategies share GeneralStrategy.Embed, so any item's strategy embeds the whole batch.
func (p processor) embedAndStoreVectors(ctx context.Context, items []embedItem) error {
	chunks := extractChunks(items)
	if len(chunks) == 0 {
		return nil
	}

	vectors, err := p.embedBatches(ctx, items[0].strategy, chunks)
	if err != nil {
		return err
	}
	embeddings, err := pairVectorsToChunks(chunks, vectors)
	if err != nil {
		return err
	}

	return p.vectorStore.Replace(ctx, embeddings)
}

// embedBatches embeds the chunks in requests of at most embedBatchSize, so a batch that
// overshot the buffer limit — or a single large document — is never one oversized request.
func (p processor) embedBatches(ctx context.Context, fileStrategy strategy.Strategy, chunks []storage.Chunk) ([][]float32, error) {
	vectors := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += p.embedBatchSize {
		end := start + p.embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		part, err := fileStrategy.Embed(ctx, chunks[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, part...)
	}

	return vectors, nil
}

func extractChunks(items []embedItem) []storage.Chunk {
	var chunks []storage.Chunk
	for _, item := range items {
		chunks = append(chunks, item.chunks...)
	}

	return chunks
}

func pairVectorsToChunks(chunks []storage.Chunk, vectors [][]float32) ([]storage.ChunkEmbedding, error) {
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("embedding count mismatch: want %d, got %d", len(chunks), len(vectors))
	}

	dimensions := len(vectors[0])
	embeddings := make([]storage.ChunkEmbedding, len(chunks))
	for i, chunk := range chunks {
		if len(vectors[i]) != dimensions {
			return nil, fmt.Errorf("embedding dimension mismatch for chunk %d", chunk.ID)
		}
		embeddings[i] = storage.ChunkEmbedding{ChunkID: chunk.ID, Vector: vectors[i]}
	}

	return embeddings, nil
}

func (p processor) markEmbedded(ctx context.Context, document storage.Document) error {
	if err := p.store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}
	p.progress.Advance()

	return nil
}
