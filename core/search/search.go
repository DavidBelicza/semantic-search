// Package search holds the public search domain types: the query configuration a caller passes,
// the result types it gets back, and the Searcher seam. They live in their own package so both the
// library facade and the internal search pipeline can share them without an import cycle.
package search

import "context"

// Searcher runs a search and returns the matching documents. It is the seam behind Engine.Search;
// supply a custom implementation through the engine config to replace the whole search flow.
type Searcher interface {
	Search(ctx context.Context, config SearchConfig) ([]DocumentResult, error)
}

// SearchConfig is the whole input to a search: the query and its optional knobs.
type SearchConfig struct {
	// Query is the search text.
	Query string
	// TaskType selects the model's query task; empty uses the model's default retrieval task.
	TaskType string
	// MinRelevance is the minimum required relevance (0 to 1). Higher means more relevant results
	// but a shorter list. Zero keeps everything.
	MinRelevance float64
	// MaxDocuments caps how many top-ranked documents the search returns. Zero uses the default (20).
	MaxDocuments int
	// MaxChunks caps how many top-ranked chunks each returned document keeps. Zero uses the default (3).
	MaxChunks int
}

// SearchResult is one match: the chunk's identity and text plus its distance score.
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}

// DocumentResult is one document match: its id, its file name and absolute path, its relevance
// score (its best chunk), and the chunks that matched inside it, ranked best first.
type DocumentResult struct {
	DocumentID   int64
	FileName     string
	AbsolutePath string
	Score        float64
	Chunks       []SearchResult
}
