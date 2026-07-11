package model

import "github.com/davidbelicza/semantic-search/core/storage"

const (
	// Qwen3SmallModelName is the model id for Qwen3 Embedding 0.6B (1024 dimensions), served over
	// an OpenAI-compatible API.
	Qwen3SmallModelName = "text-embedding-qwen3-embedding-0.6b"
	// Qwen3SmallModelDimensions is Qwen3 Embedding 0.6B's output vector size.
	Qwen3SmallModelDimensions = 1024
	// qwen3QueryInstruction is Qwen3's documented retrieval instruction, prepended to queries only.
	qwen3QueryInstruction = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "
)

// Qwen3SmallModel carries the model-specific knowledge for Qwen3 Embedding 0.6B: its id, its
// vector size, and the query instruction it requires. It does not talk to the server — transport
// is handled separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// Qwen3 asks for an "Instruct: … Query: …" instruction on the query only; indexed passages are
// embedded as-is. The title is not part of Qwen3's document template, so it is not added.
type Qwen3SmallModel struct{}

func (Qwen3SmallModel) Name() string { return Qwen3SmallModelName }

func (Qwen3SmallModel) Dimensions() int { return Qwen3SmallModelDimensions }

// BuildData embeds the chunk's text as-is; Qwen3 uses no document-side instruction.
func (Qwen3SmallModel) BuildData(chunk storage.Chunk) string {
	return chunk.Text
}

// BuildQuery prepends Qwen3's retrieval instruction to the query.
func (Qwen3SmallModel) BuildQuery(query string) string {
	return qwen3QueryInstruction + query
}
