package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"semantic-search/internal/storage/lancedb"
	storage "semantic-search/internal/storage/sqlite"
)

// SearchResult is one chunk match: the document it belongs to, the chunk id, the
// chunk text, and the score (the vector distance from the query — lower is closer).
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

type SearchMetadataStore interface {
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error)
}

type SearchVectorStore interface {
	Search(ctx context.Context, query []float32, limit int) ([]lancedb.VectorHit, error)
}

// VectorStore is the full vector-store surface used across the commands: chunk
// deletion and replacement for indexing plus similarity search.
type VectorStore interface {
	Delete(ctx context.Context, chunkIDs []int64) error
	Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error
	SearchVectorStore
}

type QueryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Search embeds the query, retrieves the nearest chunk vectors, and resolves each hit
// to its document id, chunk id, and text. Results preserve vector-similarity order.
func Search(ctx context.Context, store SearchMetadataStore, vectorStore SearchVectorStore, queryEmbedder QueryEmbedder, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be greater than zero")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search query is required")
	}

	vectors, err := queryEmbedder.Embed(ctx, []string{query})
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

func hitChunkIDs(hits []lancedb.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}

func buildSearchResults(hits []lancedb.VectorHit, metadata []storage.ChunkMetadata) []SearchResult {
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
