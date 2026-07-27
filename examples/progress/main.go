// Progress: index a folder of files and print how far the run has got.
//
// IndexOptions.OnProgress is called as indexing advances, with the phase the run is in, how
// many files that phase has finished, and how many it set out to do. In the indexing phase
// those two numbers are the files scanned and, of those, the files actually processed — the
// scan finds every file, and only the ones whose content changed are read, chunked, and
// embedded.
//
// Needs an OpenAI-compatible embedding server on http://127.0.0.1:1234 (e.g. LM
// Studio) serving EmbeddingGemma, and a C compiler (the SQLite backend uses cgo).
//
// Run from the repository root:
//
//	go run ./examples/progress
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
	dbDir, err := os.MkdirTemp("", "semantic-search-progress")
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

	// Index the sample files, rewriting a counter line as the run advances. The callback runs
	// synchronously on the indexing goroutine, so anything slow in it slows the run down.
	err = engine.Index(ctx, "examples/files", semanticsearch.IndexOptions{
		OnProgress: func(phase semanticsearch.IndexPhase, done int, total int) {
			if phase != semanticsearch.PhaseIndexing {
				return
			}
			fmt.Printf("\rprocessed %d of %d scanned files", done, total)
		},
	})
	fmt.Println()
	if err != nil {
		log.Fatal(err)
	}
}
