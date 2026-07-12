// Package search holds the public search domain types: the query configuration a caller passes
// and the result types it gets back. They live in their own package so both the library facade
// and the internal search pipeline can share them without an import cycle.
package search

// SearchConfig is the whole input to a search: the query and its optional knobs.
type SearchConfig struct {
	// Query is the search text.
	Query string
	// TaskType selects the model's query task; empty uses the model's default retrieval task.
	TaskType string
	// MaxDocuments caps how many top-ranked documents the search returns. Nil returns all.
	MaxDocuments *int
	// MaxChunks caps how many top-ranked chunks each returned document keeps. Nil keeps all.
	MaxChunks *int
}

// SearchResult is one match: the chunk's identity and text plus its distance score.
type SearchResult struct {
	DocumentID int64
	ChunkID    int64
	Title      string
	Text       string
	Score      float64
}
