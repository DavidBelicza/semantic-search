package model

import (
	"strings"

	"github.com/davidbelicza/semantic-search/core/storage"
)

const (
	// GemmaModelName is the official model id for EmbeddingGemma (768 dimensions), served over
	// an OpenAI-compatible API. It is named for precision; it is not the separate gemma-4-e2b
	// text-generation model.
	GemmaModelName = "text-embedding-embeddinggemma-300m-qat"
	// GemmaModelDimensions is EmbeddingGemma's output vector size.
	GemmaModelDimensions = 768
)

// GemmaTasks are the query task types EmbeddingGemma documents. Retrieval is the default.
// Read-only.
var GemmaTasks = struct {
	Retrieval, QuestionAnswering, FactChecking, Classification, Clustering, SentenceSimilarity, CodeRetrieval string
}{
	Retrieval:          "search result",
	QuestionAnswering:  "question answering",
	FactChecking:       "fact checking",
	Classification:     "classification",
	Clustering:         "clustering",
	SentenceSimilarity: "sentence similarity",
	CodeRetrieval:      "code retrieval",
}

// GemmaModel carries the model-specific knowledge for EmbeddingGemma: its id, its vector size,
// and the prompt templates it requires. It does not talk to the server — transport is handled
// separately by OpenAIClient — so it composes with any OpenAI-compatible client.
//
// EmbeddingGemma requires prompt templates: indexed passages use
// "title: <title> | text: <content>" and queries use "task: search result | query: <query>".
// Omitting them badly degrades ranking (junk can outrank relevant chunks).
type GemmaModel struct{}

func (GemmaModel) Name() string { return GemmaModelName }

func (GemmaModel) Dimensions() int { return GemmaModelDimensions }

// BuildData formats a chunk for indexing with EmbeddingGemma's document template. The title
// carries the chunk's heading path (or note name); an empty title becomes "none", the model's
// documented placeholder.
func (GemmaModel) BuildData(chunk storage.Chunk) string {
	label := strings.TrimSpace(chunk.Title)
	if label == "" {
		label = "none"
	}

	return "title: " + label + " | text: " + chunk.Text
}

// BuildQuery formats a search query with EmbeddingGemma's query template. The task type fills
// the "task:" field (e.g. "classification", "question answering"); an empty task type uses
// "search result", Gemma's default retrieval task.
func (GemmaModel) BuildQuery(query, taskType string) (string, error) {
	task := taskType
	if task == "" {
		task = GemmaTasks.Retrieval
	}

	return "task: " + task + " | query: " + query, nil
}
