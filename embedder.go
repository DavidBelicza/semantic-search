package semanticsearch

import (
	"github.com/davidbelicza/semantic-search/core/embedder"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

// Standard identifies the wire protocol an AI embedder speaks. Most embedding servers are
// OpenAI-compatible; other standards can be added later without changing callers.
type Standard string

// StandardOpenAI is the OpenAI-compatible /v1/embeddings protocol (LM Studio, Ollama, and
// most local servers).
const StandardOpenAI Standard = "openai"

// AiEmbedderConfig configures an AI embedder: which protocol to speak, and the endpoint,
// model, and vector size to use.
type AiEmbedderConfig struct {
	Standard   Standard
	BaseURL    string
	Model      string
	Dimensions int
}

// NewAiEmbedder builds an embedder for the given standard. It returns nil for an unknown
// standard; NewEngine rejects a nil embedder.
func NewAiEmbedder(config AiEmbedderConfig) strategy.Embedder {
	if config.Standard != StandardOpenAI {
		return nil
	}

	client := embedder.NewOpenAIEmbedder(config.BaseURL, config.Model)
	if config.Dimensions > 0 {
		client.Dimensions = config.Dimensions
	}

	return client
}
