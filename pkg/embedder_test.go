package semanticsearch

import "testing"

func TestNewAiEmbedderOpenAI(t *testing.T) {
	e := NewAiEmbedder(AiEmbedderConfig{
		Standard:   StandardOpenAI,
		BaseURL:    "http://127.0.0.1:1234",
		Model:      "embeddinggemma-300m",
		Dimensions: 768,
	})
	if e == nil {
		t.Fatal("expected an embedder for the OpenAI standard")
	}
}

func TestNewAiEmbedderUnknownStandardIsNil(t *testing.T) {
	if NewAiEmbedder(AiEmbedderConfig{Standard: "nope"}) != nil {
		t.Fatal("expected nil for an unknown standard")
	}
}
