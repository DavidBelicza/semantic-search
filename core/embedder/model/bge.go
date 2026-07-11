package model

import "github.com/davidbelicza/semantic-search/core/storage"

const (
	// BGELargeModelName is the model id for BGE large en v1.5 (1024 dimensions), served over an
	// OpenAI-compatible API.
	BGELargeModelName = "text-embedding-bge-large-en-v1.5"
	// BGELargeModelDimensions is BGE large en v1.5's output vector size.
	BGELargeModelDimensions = 1024
	// bgeQueryInstruction is BGE's documented retrieval instruction, prepended to queries only.
	bgeQueryInstruction = "Represent this sentence for searching relevant passages: "
)

// BGELargeModel carries the model-specific knowledge for BGE large en v1.5: its id, its vector
// size, and the query instruction it requires. It does not talk to the server — transport is
// handled separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// BGE asks for an instruction on the query only; indexed passages are embedded as-is. The title
// is not part of BGE's document template, so it is not added.
type BGELargeModel struct{}

func (BGELargeModel) Name() string { return BGELargeModelName }

func (BGELargeModel) Dimensions() int { return BGELargeModelDimensions }

// BuildData embeds the chunk's text as-is; BGE uses no document-side prefix.
func (BGELargeModel) BuildData(chunk storage.Chunk) string {
	return chunk.Text
}

// BuildQuery prepends BGE's retrieval instruction to the query. BGE embeds queries in a single
// mode, so a non-empty task type is rejected rather than silently ignored.
func (BGELargeModel) BuildQuery(query, taskType string) (string, error) {
	if taskType != "" {
		return "", unsupportedTaskType(BGELargeModelName)
	}

	return bgeQueryInstruction + query, nil
}
