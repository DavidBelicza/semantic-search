package semanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"semantic-search/internal/embedder"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/storage/sqlitevec"
)

// SearchResult is one chunk match: the document it belongs to, the chunk id, its title
// and text, and the score (vector distance from the query — lower is closer).
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

type searchMetadataStore interface {
	ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error)
}

type searchVectorStore interface {
	Search(ctx context.Context, query []float32, limit int) ([]sqlitevec.VectorHit, error)
}

type queryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Search instantiates the stores and query embedder, embeds the query, retrieves the
// nearest chunk vectors, and resolves each hit to its document, chunk, and text. Results
// preserve vector-similarity order.
func Search(ctx context.Context, dbPath string, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be greater than zero")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search query is required")
	}

	store, vectorStore, err := openStores(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	defer vectorStore.Close()

	return search(ctx, store, vectorStore, newQueryEmbedder(), query, limit)
}

func newQueryEmbedder() queryEmbedder {
	e := embedder.NewOpenAIEmbedder(embedder.DefaultBaseURL, embedder.DefaultModel)
	e.Dimensions = embedder.DefaultDimensions
	e.Prefix = embedder.QueryPrefix
	return e
}

func search(ctx context.Context, store searchMetadataStore, vectorStore searchVectorStore, embedder queryEmbedder, query string, limit int) ([]SearchResult, error) {
	vectors, err := embedder.Embed(ctx, []string{query})
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

func hitChunkIDs(hits []sqlitevec.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}

func buildSearchResults(hits []sqlitevec.VectorHit, metadata []storage.ChunkMetadata) []SearchResult {
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
