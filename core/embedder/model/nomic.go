package model

import "github.com/davidbelicza/semantic-search/core/storage"

const (
	// NomicModelName is the model id for Nomic Embed Text v1.5 (768 dimensions), served over an
	// OpenAI-compatible API.
	NomicModelName = "text-embedding-nomic-embed-text-v1.5"
	// NomicModelDimensions is Nomic Embed Text v1.5's output vector size.
	NomicModelDimensions = 768
)

// NomicModel carries the model-specific knowledge for Nomic Embed Text v1.5: its id, its vector
// size, and the task prefixes it requires. It does not talk to the server — transport is handled
// separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// Nomic requires task prefixes: indexed passages use the "search_document: " prefix and queries
// use "search_query: ". The title is not part of Nomic's document template, so it is not added.
type NomicModel struct{}

func (NomicModel) Name() string { return NomicModelName }

func (NomicModel) Dimensions() int { return NomicModelDimensions }

// BuildData formats a chunk for indexing with Nomic's document prefix.
func (NomicModel) BuildData(chunk storage.Chunk) string {
	return "search_document: " + chunk.Text
}

// BuildQuery formats a search query with Nomic's query prefix.
func (NomicModel) BuildQuery(query string) string {
	return "search_query: " + query
}
