package model

import "github.com/davidbelicza/semantic-search/core/storage"

const (
	// MxbaiLargeModelName is the model id for mxbai embed large v1 (1024 dimensions), served over
	// an OpenAI-compatible API.
	MxbaiLargeModelName = "text-embedding-mxbai-embed-large-v1"
	// MxbaiLargeModelDimensions is mxbai embed large v1's output vector size.
	MxbaiLargeModelDimensions = 1024
	// mxbaiQueryInstruction is mxbai's documented retrieval instruction, prepended to queries only.
	mxbaiQueryInstruction = "Represent this sentence for searching relevant passages: "
)

// MxbaiLargeModel carries the model-specific knowledge for mxbai embed large v1: its id, its
// vector size, and the query instruction it requires. It does not talk to the server — transport
// is handled separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// mxbai asks for an instruction on the query only; indexed passages are embedded as-is. The title
// is not part of mxbai's document template, so it is not added.
type MxbaiLargeModel struct{}

func (MxbaiLargeModel) Name() string { return MxbaiLargeModelName }

func (MxbaiLargeModel) Dimensions() int { return MxbaiLargeModelDimensions }

// BuildData embeds the chunk's text as-is; mxbai uses no document-side prefix.
func (MxbaiLargeModel) BuildData(chunk storage.Chunk) string {
	return chunk.Text
}

// BuildQuery prepends mxbai's retrieval instruction to the query.
func (MxbaiLargeModel) BuildQuery(query string) string {
	return mxbaiQueryInstruction + query
}
