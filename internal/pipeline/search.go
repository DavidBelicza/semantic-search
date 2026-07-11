package pipeline

import (
	"context"
	"fmt"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

// SearchStore is the metadata surface search needs — a subset of storage.Storage, which any
// injected store satisfies.
type SearchStore interface {
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error)
}

// SearchVectorStore is the vector query surface search needs — a subset of storage.VectorStorage.
type SearchVectorStore interface {
	Search(ctx context.Context, query []float32, limit int) ([]storage.VectorHit, error)
}

// SearchResult is one match: the chunk's identity and text plus its distance score.
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

// Search is the query pipeline: it phrases the query for the model, embeds it, runs the vector
// nearest-neighbor lookup, and resolves the hits back to chunk text and metadata. An empty
// taskType uses the model's default retrieval task.
func Search(ctx context.Context, store SearchStore, vectorStore SearchVectorStore, model strategy.EmbeddingModel, client strategy.AiClient, query string, taskType string, limit int) ([]SearchResult, error) {
	phrased, err := model.BuildQuery(query, taskType)
	if err != nil {
		return nil, err
	}

	vectors, err := client.Embed(ctx, []string{phrased})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("expected one query embedding, got %d", len(vectors))
	}

	hits, err := vectorStore.Search(ctx, vectors[0], limit)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	metadata, err := store.ChunkMetadataByIDs(ctx, hitChunkIDs(hits))
	if err != nil {
		return nil, err
	}

	return buildSearchResults(hits, metadata), nil
}

// buildSearchResults resolves vector hits to their chunk metadata, preserving hit order and
// skipping any hit whose metadata is missing.
func buildSearchResults(hits []storage.VectorHit, metadata []storage.ChunkMetadata) []SearchResult {
	byID := make(map[int64]storage.ChunkMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.ChunkID] = item
	}

	results := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		item, ok := byID[hit.ChunkID]
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			DocumentID: item.DocumentID,
			ChunkID:    item.ChunkID,
			Title:      item.Title,
			Text:       item.Text,
			Score:      hit.Distance,
		})
	}

	return results
}

// hitChunkIDs pulls the chunk ids out of the hits, in order.
func hitChunkIDs(hits []storage.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}
