// Package pipeline owns the format-agnostic orchestration of document processing: the
// status state machine, chunk-level change detection (reconciliation), embedding, and
// SQLite / vector-store writes. It calls a strategy's public steps for per-format work
// and an injected embedder for vectors; it contains no format-specific logic.
package pipeline

import (
	"context"
	"errors"
	"fmt"

	"semantic-search/internal/embedder"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/strategy"
)

const batchSize = 1

// Store is the metadata surface the processing pipeline needs.
type Store interface {
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error)
	ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error)
	ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error)
	UpdateDocumentStatus(ctx context.Context, fileID string, status string) error
	MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error
}

// VectorStore is the write surface of the vector index.
type VectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error
}

// Embedder turns chunk texts into vectors. It is injected, not owned by any strategy.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Result struct {
	Processed int
	Embedded  int
}

// processingPipeline is "pipeline 2": it runs file by file over documents that need
// work, resolving each document's strategy from the pool and embedding with the injected
// embedder.
type processingPipeline struct {
	store       Store
	vectorStore VectorStore
	pool        strategy.Pool
	embedder    Embedder
}

func newProcessingPipeline(store Store, vectorStore VectorStore, pool strategy.Pool, embedder Embedder) *processingPipeline {
	return &processingPipeline{store: store, vectorStore: vectorStore, pool: pool, embedder: embedder}
}

// Process runs the document-processing pipeline: it drives each document through the
// strategy's remaining steps in sequence — read, parse, chunk — then reconciles and
// embeds. Scanned documents are chunked and embedded; any left in the chunked state (a
// previous run that embedded partially) are then embedded.
func Process(ctx context.Context, store Store, vectorStore VectorStore, pool strategy.Pool, embedder Embedder, failFast bool) error {
	p := newProcessingPipeline(store, vectorStore, pool, embedder)

	if _, err := p.processScanned(ctx, failFast); err != nil {
		return err
	}

	_, err := p.processChunked(ctx, failFast)
	return err
}

// processScanned chunks and embeds every scanned document, advancing it to embedded.
func (p *processingPipeline) processScanned(ctx context.Context, failFast bool) (Result, error) {
	return p.processByStatus(ctx, storage.DocumentStatusScanned, failFast, p.processScannedDocument)
}

// processChunked embeds every chunked document (chunks already exist) and marks it
// embedded.
func (p *processingPipeline) processChunked(ctx context.Context, failFast bool) (Result, error) {
	return p.processByStatus(ctx, storage.DocumentStatusChunked, failFast, p.embedChunkedDocument)
}

type documentProcessor func(ctx context.Context, document storage.Document) (bool, error)

// processByStatus walks every document in a status, one page at a time, ordered by
// ascending id. With failFast unset, a per-document error is collected and processing
// continues; the joined errors are returned at the end. Paginating by id means a
// document that failed (and kept its status) is not revisited this run.
func (p *processingPipeline) processByStatus(ctx context.Context, status string, failFast bool, process documentProcessor) (Result, error) {
	var result Result
	var errs []error
	var afterID int64

	for {
		documents, err := p.store.DocumentsByStatus(ctx, status, afterID, batchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			embedded, err := process(ctx, document)
			if err != nil && failFast {
				return result, err
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if embedded {
				result.Embedded++
			}
			result.Processed++
		}
	}
}

func (p *processingPipeline) processScannedDocument(ctx context.Context, document storage.Document) (bool, error) {
	fileStrategy, ok := p.pool.Find(document.AbsolutePath)
	if !ok {
		return false, fmt.Errorf("no strategy for document %q", document.AbsolutePath)
	}

	chunks, err := readParseChunk(ctx, fileStrategy, document)
	if err != nil {
		return false, fmt.Errorf("process document %q: %w", document.AbsolutePath, err)
	}

	existingChunks, err := p.store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return false, fmt.Errorf("load existing chunks for %q: %w", document.AbsolutePath, err)
	}

	plan := storage.ReconcileChunks(existingChunks, chunks)

	insertedChunks, err := p.store.ApplyDocumentChunkReconcile(ctx, document.ID, plan)
	if err != nil {
		return false, fmt.Errorf("reconcile chunks for %q: %w", document.AbsolutePath, err)
	}

	// SQLite is the source of truth: commit the chunk rows before mutating the vector
	// store, so a crash cannot leave chunk rows pointing at vectors that were already
	// deleted.
	if err := p.vectorStore.Delete(ctx, plan.RemoveIDs); err != nil {
		return false, fmt.Errorf("delete old vectors for %q: %w", document.AbsolutePath, err)
	}

	if err := p.store.UpdateDocumentStatus(ctx, document.FileID, storage.DocumentStatusChunked); err != nil {
		return false, fmt.Errorf("mark document chunked %q: %w", document.AbsolutePath, err)
	}

	chunksToEmbed, err := p.chunksForEmbedding(ctx, document, insertedChunks, existingChunks)
	if err != nil {
		return false, err
	}

	embedded, err := p.embedIfNeeded(ctx, document, chunksToEmbed)
	if err != nil {
		return false, err
	}

	if err := p.store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return false, fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}

	return embedded, nil
}

// readParseChunk sequences a strategy's steps for one document. Sequencing is
// orchestration, so it lives in the pipeline; the strategy only provides the steps.
func readParseChunk(ctx context.Context, fileStrategy strategy.Strategy, document storage.Document) ([]storage.Chunk, error) {
	text, err := fileStrategy.Read(ctx, document)
	if err != nil {
		return nil, err
	}

	parsedText, err := fileStrategy.Parse(ctx, text)
	if err != nil {
		return nil, err
	}

	return fileStrategy.Chunk(ctx, document, parsedText)
}

func (p *processingPipeline) embedChunkedDocument(ctx context.Context, document storage.Document) (bool, error) {
	chunks, err := p.store.ChunksByDocumentID(ctx, document.ID)
	if err != nil {
		return false, fmt.Errorf("load chunks for %q: %w", document.AbsolutePath, err)
	}

	embedded, err := p.embedIfNeeded(ctx, document, chunks)
	if err != nil {
		return false, err
	}

	if err := p.store.MarkDocumentEmbedded(ctx, document.FileID, document.ContentHash); err != nil {
		return false, fmt.Errorf("mark document embedded %q: %w", document.AbsolutePath, err)
	}

	return embedded, nil
}

// chunksForEmbedding selects which chunks still need vectors. A document that was
// already embedded keeps valid vectors for its unchanged (kept) chunks, so only the
// newly inserted chunks need embedding — this avoids re-embedding an entire document
// when only its file metadata changed. A document that has never been embedded must
// embed all of its current chunks, even when reconciliation kept them, because no
// vectors exist for them yet.
func (p *processingPipeline) chunksForEmbedding(ctx context.Context, document storage.Document, inserted []storage.Chunk, existing []storage.Chunk) ([]storage.Chunk, error) {
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

func (p *processingPipeline) embedIfNeeded(ctx context.Context, document storage.Document, chunks []storage.Chunk) (bool, error) {
	if len(chunks) == 0 {
		return false, nil
	}
	if p.embedder == nil {
		return false, fmt.Errorf("embedder is required for document %q", document.AbsolutePath)
	}
	if err := p.embedChunks(ctx, document, chunks); err != nil {
		return false, err
	}

	return true, nil
}

func (p *processingPipeline) embedChunks(ctx context.Context, document storage.Document, chunks []storage.Chunk) error {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = embedder.DocumentInput(chunk.Title, chunk.Text)
	}

	vectorValues, err := p.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed chunks for %q: %w", document.AbsolutePath, err)
	}
	if len(vectorValues) != len(chunks) {
		return fmt.Errorf("embedding count mismatch for %q: want %d, got %d", document.AbsolutePath, len(chunks), len(vectorValues))
	}

	dimensions := len(vectorValues[0])
	embeddings := make([]storage.ChunkEmbedding, len(chunks))
	for i, chunk := range chunks {
		if len(vectorValues[i]) != dimensions {
			return fmt.Errorf("embedding dimension mismatch for %q chunk %d", document.AbsolutePath, chunk.ChunkIndex)
		}
		embeddings[i] = storage.ChunkEmbedding{ChunkID: chunk.ID, Vector: vectorValues[i]}
	}

	if err := p.vectorStore.Replace(ctx, embeddings); err != nil {
		return fmt.Errorf("store vectors for %q: %w", document.AbsolutePath, err)
	}

	return nil
}
