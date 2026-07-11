package model

import "github.com/davidbelicza/semantic-search/core/storage"

const (
	// E5LargeModelName is the model id for Multilingual E5 large (1024 dimensions), served over an
	// OpenAI-compatible API.
	E5LargeModelName = "text-embedding-multilingual-e5-large"
	// E5LargeModelDimensions is Multilingual E5 large's output vector size.
	E5LargeModelDimensions = 1024
)

// E5LargeModel carries the model-specific knowledge for Multilingual E5 large: its id, its vector
// size, and the task prefixes it requires. It does not talk to the server — transport is handled
// separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// E5 requires task prefixes: indexed passages use the "passage: " prefix and queries use
// "query: ". The title is not part of E5's document template, so it is not added.
type E5LargeModel struct{}

func (E5LargeModel) Name() string { return E5LargeModelName }

func (E5LargeModel) Dimensions() int { return E5LargeModelDimensions }

// BuildData formats a chunk for indexing with E5's document prefix.
func (E5LargeModel) BuildData(chunk storage.Chunk) string {
	return "passage: " + chunk.Text
}

// BuildQuery formats a search query with E5's query prefix. E5 embeds queries in a single mode,
// so a non-empty task type is rejected rather than silently ignored.
func (E5LargeModel) BuildQuery(query, taskType string) (string, error) {
	if taskType != "" {
		return "", unsupportedTaskType(E5LargeModelName)
	}

	return "query: " + query, nil
}
