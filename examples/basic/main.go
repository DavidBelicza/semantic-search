// Basic search: index a folder of files into an on-disk SQLite database, then
// run a semantic search over it and print the matching documents.
//
// Needs an OpenAI-compatible embedding server on http://127.0.0.1:1234 (e.g. LM
// Studio) serving EmbeddingGemma, and a C compiler (the SQLite backend uses cgo).
//
// Run from the repository root:
//
//	go run ./examples/basic
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

	// A throwaway directory for the two SQLite databases.
	dbDir, err := os.MkdirTemp("", "semantic-search-basic")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dbDir)

	// The model formats text for EmbeddingGemma; the embedder sends it to the server.
	model := semanticsearch.NewModel(semanticsearch.Gemma300mQAT)

	// The metadata store holds documents and chunks; the vector store holds embeddings.
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

	// Compose the engine from the model, the embedder, the two stores, and a markdown strategy.
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

	// Index the sample files.
	if err := engine.Index(ctx, "examples/files", semanticsearch.IndexOptions{}); err != nil {
		log.Fatal(err)
	}

	// Search by meaning: the query shares no words with the file but still matches it.
	docs, err := engine.Search(ctx, semanticsearch.SearchConfig{Query: "how do I fall asleep faster"})
	if err != nil {
		log.Fatal(err)
	}

	for _, doc := range docs {
		fmt.Printf("%.4f  %s\n", doc.Score, doc.FileName)
	}
}
