// Postgres search: index the sample files into PostgreSQL with the pgvector
// extension, then run a semantic search. This is the server-side setup; the only
// difference from the SQLite examples is the two store constructors.
//
// Needs an OpenAI-compatible embedding server on http://127.0.0.1:1234 (e.g. LM
// Studio) serving EmbeddingGemma, and a running PostgreSQL with pgvector. The pgx
// driver is pure Go, so no cgo here.
//
// Start the bundled database (Postgres + pgvector) with Docker, then run from the
// repository root:
//
//	docker compose -f test/docker/docker-compose.yml up -d
//	go run ./examples/postgres
package main

import (
	"context"
	"fmt"
	"log"

	semanticsearch "github.com/davidbelicza/semantic-search"
)

func main() {
	ctx := context.Background()

	// Connection string for the bundled Docker database.
	dsn := "postgres://semanticsearch:semanticsearch@127.0.0.1:5432/semanticsearch?sslmode=disable"

	model := semanticsearch.NewModel(semanticsearch.Gemma300mQAT)

	// Both stores live in PostgreSQL; PostgresKNN is exact search over pgvector.
	store, err := semanticsearch.NewPostgresStorage(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	vectors, err := semanticsearch.NewPostgresVectorStorage(ctx, dsn, 768, semanticsearch.PostgresKNN)
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

	docs, err := engine.Search(ctx, semanticsearch.SearchConfig{Query: "planning a trip abroad"})
	if err != nil {
		log.Fatal(err)
	}

	for _, doc := range docs {
		fmt.Printf("%.4f  %s\n", doc.Score, doc.FileName)
	}
}
