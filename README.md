<p align="center">
  <img src=".github/banner.jpeg" alt="Semantic Search: meaning over keywords" width="620">
</p>

<h1 align="center">Semantic Search</h1>

<p align="center">
  <a href="https://github.com/DavidBelicza/semantic-search/actions/workflows/ci.yml"><img src="https://github.com/DavidBelicza/semantic-search/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/DavidBelicza/semantic-search/releases"><img src="https://img.shields.io/github/v/release/DavidBelicza/semantic-search?sort=semver" alt="Release"></a>
  <a href="https://pkg.go.dev/github.com/davidbelicza/semantic-search"><img src="https://pkg.go.dev/badge/github.com/davidbelicza/semantic-search.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/DavidBelicza/semantic-search" alt="License"></a>
</p>

<p align="center">This is a <strong>semantic search library</strong> inspired by <strong>Google's Discovery Engine</strong> (Google AI Search) and by the retrieval systems behind products like <strong>Google Search</strong> and <strong>NotebookLM</strong>.
<br>It <strong>recursively indexes</strong> PDF, Markdown, code, and many other file types in a directory, then <strong>chunks</strong> them and stores them in a <strong>vector database</strong> using an <strong>embedding AI model</strong>.
<br>It enables <strong>meaning-based search</strong> across your documents and works for both <strong>client-side</strong> and <strong>server-side</strong> solutions. Written in <strong>Go</strong>, it is <strong>portable</strong> and compiles easily to any platform, OS, client, or server.</p>

## Use cases

Semantic Search works both as an embedded engine inside client apps (using the SQLite store,
on disk or in memory) and as a server-side knowledge base (using PostgreSQL and pgvector).

| Target | Use case |
|---|---|
| Desktop apps | Client-side RAG over a personal knowledge base: Google NotebookLM-like search built into the app, backed by the embedded SQLite database. |
| Mobile apps | The same personal knowledge base and meaning-based search running on-device, with no server, on the embedded SQLite database. |
| CLI tools | Terminal-based semantic search over local files and notes, backed by the embedded SQLite database. |
| Server-side | Meaning-based knowledge bases for web systems, for example integrating into webshops or product catalogs, backed by PostgreSQL and pgvector. |

## Supported formats

| Format | Extensions | How it's chunked |
|---|---|---|
| Markdown | `.md`, `.markdown`, `.mdown` | Split by headings, with code blocks kept whole |
| PDF | `.pdf` | Headings detected from font sizes, read in natural page order |
| Plain text | `.txt`, `.text`, `.log`, `.rst`, `.org`, `.adoc` | Split into overlapping paragraphs |
| Code | `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.py`, `.php`, `.java`, `.rb`, `.rs`, `.c`, `.h`, `.cpp`, `.hpp`, `.cs`, `.sh`, `.sql` | One section per function or class, titled with its full path |
| DOCX | `.docx` | Split by Word heading styles |

## How it works

1. **Index**: walk the tree; the strategy pool picks the strategy that claims each file.
2. **Parse**: decode bytes into heading/definition-structured sections.
3. **Chunk**: pack sections into token-budget chunks with overlap, each carrying its title path.
4. **Embed**: turn chunks into vectors via the embedding server.
5. **Search**: embed the query and rank chunks by vector distance using exact k-nearest-neighbor (kNN) search, comparing against every chunk for precise results.

## Requirements

- **Every use case** needs an **OpenAI-compatible embedding server**: on your own machine (LM Studio, Ollama, or llama.cpp), or on a remote host (Google AI Studio or any other server that speaks the standard protocol).
- **For client-side apps** (desktop, mobile, CLI), you also need a **C compiler**,
  because cgo builds `mattn/go-sqlite3` and the `sqlite-vec` bindings from source:
  - **macOS**: `xcode-select --install` (Clang)
  - **Debian / Ubuntu**: `sudo apt install build-essential`
  - **Fedora / RHEL**: `sudo dnf install gcc`
  - **Windows**: install a MinGW-w64 gcc toolchain (e.g. via MSYS2) and add it to `PATH`
  - **Windows (alternative)**: use [WSL2](https://learn.microsoft.com/windows/wsl/install) and
    follow the Debian / Ubuntu steps inside your Linux distribution
- **For server-side apps**: pure Go, so no C compiler is needed. You need a **PostgreSQL server
  with the pgvector extension** (`test/docker/docker-compose.yml` provides a working example).

## Install, build, test, lint

Add the library to your module:

```sh
go get github.com/davidbelicza/semantic-search
```

Working on the library itself:

```sh
go build ./...   # build (cgo)
make test        # go test ./...
make lint        # golangci-lint
```

## Usage

Semantic Search is a library. Copy the following into a Go file (for example `main.go`) to get
started: it composes an engine from an embedder, a metadata store, a vector store, and the
strategies you want, then indexes a directory and searches it.

### Full example

For CLI, desktop, or mobile apps, the recommended setup is an embedded SQLite database.

```go
package main

import (
	"context"
	"fmt"

	"github.com/davidbelicza/semantic-search"
)

func main() {
	// Configure the search engine. You compose it from an embedder that turns
	// text into vectors, a metadata store, a vector store, and the strategies
	// that decide which file types are handled and how each one is parsed and
	// chunked.
	ctx := context.Background()
	store, _ := semanticsearch.NewSQLiteStorage(ctx, "index.db")
	defer store.Close()
	vectors, _ := semanticsearch.NewSQLiteVectorStorage(ctx, "vectors.db", 768)
	defer vectors.Close()
	model := semanticsearch.NewModel(semanticsearch.Gemma300mQAT)

	engine, err := semanticsearch.NewEngine(semanticsearch.Config{
		Model: model,
		Embedder: semanticsearch.NewAiEmbedder(semanticsearch.AiEmbedderConfig{
			Standard: semanticsearch.StandardOpenAI,
			BaseURL:  "http://127.0.0.1:1234",
		}, model),
		Storage:       store,
		VectorStorage: vectors,
		Strategies: []semanticsearch.StrategyFactory{
			semanticsearch.NewMarkdownStrategy(),
			semanticsearch.NewPDFStrategy(),
			semanticsearch.NewCodeStrategy(),
			semanticsearch.NewDocxStrategy(),
			semanticsearch.NewTextStrategy(),
		},
	})
	if err != nil {
		panic(err)
	}

	// Index the directory. The engine maps the directory recursively, parses
	// every supported file, splits each one into chunks, and embeds those
	// chunks into vectors with the AI model.
	if err := engine.Index(ctx, "./docs", semanticsearch.IndexOptions{}); err != nil {
		panic(err)
	}

	// Search the indexed content. The query is embedded the same way, and
	// the engine returns the chunks whose meaning is closest to it, so
	// results are matched by meaning rather than exact keywords.
	results, _ := engine.Search(ctx, "how do I detect security threats in logs", 5)
	for _, r := range results {
		fmt.Printf("%s  (score %.4f)\n%s\n", r.Title, r.Score, r.Text)
	}
}
```

### In-memory SQLite (single process)

Alternatively, give both stores an in-memory DSN to keep everything in RAM. Because the data lives only in this process, you must index and search in the same run. Only the two store lines change:

```go
store, _ := semanticsearch.NewSQLiteStorage(ctx, "file:meta?mode=memory&cache=shared")
defer store.Close()
vectors, _ := semanticsearch.NewSQLiteVectorStorage(ctx, "file:vec?mode=memory&cache=shared", 768)
defer vectors.Close()
```

### Server-side setup with PostgreSQL and pgvector

You can run this library server-side. In that case it is recommended to switch to a multi-process SQL database by swapping the two store constructors for their PostgreSQL equivalents. The server must have the [pgvector](https://github.com/pgvector/pgvector) extension; for local development, a ready-to-use database is provided:

```sh
docker compose -f test/docker/docker-compose.yml up -d
```

Only the two store lines change:

```go
dsn := "postgres://semanticsearch:semanticsearch@127.0.0.1:5432/semanticsearch?sslmode=disable"
store, _ := semanticsearch.NewPostgresStorage(ctx, dsn)
defer store.Close()
vectors, _ := semanticsearch.NewPostgresVectorStorage(ctx, dsn, 768, semanticsearch.PostgresKNN)
defer vectors.Close()
```

The pgvector driver is pure Go, so a Postgres-only build (importing neither SQLite store) needs
no cgo and no C compiler.

### Scaling up with HNSW

If your vector database runs on the server side, you can reasonably scale it up. To do that, use `PostgresHNSW` instead of `PostgresKNN`: it builds an [HNSW](https://github.com/pgvector/pgvector#hnsw) index for approximate nearest-neighbor search, which is sub-linear and much faster at scale. Only the vector store line changes:

```go
vectors, _ := semanticsearch.NewPostgresVectorStorage(ctx, dsn, 768, semanticsearch.PostgresHNSW)
```

### Choosing an embedder model

The model interface defines the model's name, dimension size, data structure format, and search query format. The example uses Gemma, which has 300 million parameters and 768 dimensions. It is a reasonable embedder model that can run locally. **Changing models can significantly impact your application’s performance.**

```go
model := semanticsearch.NewModel(semanticsearch.Gemma300mQAT)
```

There are other pre-defined models available in this library:

- `semanticsearch.NewModel(semanticsearch.Gemma300mQAT)` loads **text-embedding-embeddinggemma-300m-qat** (768 dim)
- `semanticsearch.NewModel(semanticsearch.Nomic768)` loads **text-embedding-nomic-embed-text-v1.5** (768 dim)
- `semanticsearch.NewModel(semanticsearch.E5Large1024)` loads **text-embedding-multilingual-e5-large** (1024 dim)
- `semanticsearch.NewModel(semanticsearch.BGELarge1024)` loads **text-embedding-bge-large-en-v1.5** (1024 dim)
- `semanticsearch.NewModel(semanticsearch.Qwen30_6B1024)` loads **text-embedding-qwen3-embedding-0.6b** (1024 dim)
- `semanticsearch.NewModel(semanticsearch.MxbaiLarge1024)` loads **text-embedding-mxbai-embed-large-v1** (1024 dim)

For any other model that needs no prompt templates, use `NewGeneralModel` with the model id and vector size. Switching models or dimensions is just a different argument.

```go
model := semanticsearch.NewGeneralModel("text-embedding-nomic-embed-text-v1.5", 768)
```

If a model needs its own prompt templates, implement the `EmbeddingModel` interface and inject it.

```go
type myModel struct{}

func (myModel) Name() string       { return "my-embedding-model" }
func (myModel) Dimensions() int    { return 1024 }
func (myModel) BuildData(chunk storage.Chunk) string { return chunk.Text }
func (myModel) BuildQuery(query string) string       { return query }

// semanticsearch.NewEngine(semanticsearch.Config{ Model: myModel{}, ... })
```

### Custom AI client

The built-in `NewAiEmbedder` returns an `OpenAIClient` that speaks the OpenAI-compatible protocol with an optional `APIKey` (sent as a Bearer token). For anything it does not cover, such as rotating OAuth tokens (e.g. production Vertex AI), request signing (e.g. AWS Bedrock), or a non-OpenAI wire format, implement the `AiClient` interface yourself and inject it. It is a single method:

```go
type myClient struct {
	// your HTTP client, credentials, token cache, etc.
}

func (c myClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	// Refresh your OAuth token / sign the request here, call your provider, and
	// return one vector per input text, in the same order.
}

// Inject it like any other client:
// semanticsearch.NewEngine(semanticsearch.Config{ Embedder: myClient{}, ... })
```

## Documents

### Reference

- [docs/architecture.md](docs/architecture.md): how the pipeline and strategies fit together.
- [docs/chunking.md](docs/chunking.md): how each format is parsed and chunked.

### Research

- [docs/research/vector-search-scaling.md](docs/research/vector-search-scaling.md): indexing and search performance, limits, and scaling options.
- [docs/research/code-parsing-scaling.md](docs/research/code-parsing-scaling.md): measurements for code parsing, compared with the book corpus.
- [docs/research/sqlite-vec-migration.md](docs/research/sqlite-vec-migration.md): moving the vector store to sqlite-vec.
- [docs/research/pdf-extraction-engine.md](docs/research/pdf-extraction-engine.md): PDF text extraction with PDFium.

## License

Released under the [MIT License](LICENSE).
