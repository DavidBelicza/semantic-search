package embedder

import "context"

// EmbeddingGemma300MQATEmbedder embeds document chunks using the EmbeddingGemma model
// (text-embedding-embeddinggemma-300m-qat, 768 dimensions) served over the LM Studio
// OpenAI-compatible API. It is named after the official model id for precision; it is
// not the separate gemma-4-e2b text-generation model.
//
// It is a document embedder: callers format each chunk with DocumentInput before
// embedding. Query embedding uses QueryPrefix separately (see the search path).
type EmbeddingGemma300MQATEmbedder struct {
	client OpenAIEmbedder
}

// NewEmbeddingGemma300MQATEmbedder builds the preconfigured document embedder pointed at
// the given base URL (empty falls back to DefaultBaseURL).
func NewEmbeddingGemma300MQATEmbedder(baseURL string) *EmbeddingGemma300MQATEmbedder {
	client := NewOpenAIEmbedder(baseURL, DefaultModel)
	client.Dimensions = DefaultDimensions

	return &EmbeddingGemma300MQATEmbedder{client: client}
}

func (e *EmbeddingGemma300MQATEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.client.Embed(ctx, texts)
}
