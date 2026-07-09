package semanticsearch

import "github.com/davidbelicza/semantic-search/core/storage"

// SearchResult is one chunk match: the document it belongs to, the chunk id, its title
// and text, and the score (vector distance from the query — lower is closer).
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

func hitChunkIDs(hits []storage.VectorHit) []int64 {
	ids := make([]int64, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}

	return ids
}

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
