// Search config: index the sample files, then run a search that tunes the
// result set with SearchConfig — a task type, a minimum relevance, and caps on
// how many documents and chunks come back.
//
// Needs an OpenAI-compatible embedding server on http://127.0.0.1:1234 (e.g. LM
// Studio) serving EmbeddingGemma, and a C compiler (the SQLite backend uses cgo).
//
// Run from the repository root:
//
//	go run ./examples/searchconfig
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	semanticsearch "github.com/davidbelicza/semantic-search"
)

func main() {
	ctx := context.Background()

	dbDir, err := os.MkdirTemp("", "semantic-search-config")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dbDir)

	model := semanticsearch.NewModel(semanticsearch.Gemma300mQAT)

	store, err := semanticsearch.NewSQLiteStorage(ctx, filepath.Join(dbDir, "index.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	vectors, err := semanticsearch.NewSQLiteVectorStorage(ctx, filepath.Join(dbDir, "vectors.db"), 768)
	if err != nil {
		log.Fatal(err)
	}
	defer vectors.Close()

	engine, err := semanticsearch.NewEngine(semanticsearch.Config{
		Model:         model,
		Embedder:      semanticsearch.NewAiEmbedder(semanticsearch.AiEmbedderConfig{Standard: semanticsearch.StandardOpenAI, BaseURL: "http://127.0.0.1:1234"}, model),
		Storage:       store,
		VectorStorage: vectors,
		Strategies:    []semanticsearch.StrategyFactory{semanticsearch.NewMarkdownStrategy()},
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := engine.Index(ctx, "examples/files", semanticsearch.IndexOptions{}); err != nil {
		log.Fatal(err)
	}

	// TaskType phrases the query for the model; MinRelevance drops weak matches;
	// MaxDocuments and MaxChunks cap how much comes back.
	docs, err := engine.Search(ctx, semanticsearch.SearchConfig{
		Query:        "ways to stop overwatering my plants",
		TaskType:     semanticsearch.TaskGemma.QuestionAnswering,
		MinRelevance: 0.3,
		MaxDocuments: 3,
		MaxChunks:    2,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Each document carries the chunks that matched inside it, most relevant first.
	for _, doc := range docs {
		fmt.Printf("%.4f  %s  (%d chunks)\n", doc.Score, doc.FileName, len(doc.Chunks))
	}
}
