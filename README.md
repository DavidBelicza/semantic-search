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

## Contents

- [What semantic search is](#what-semantic-search-is)
- [Use cases](#use-cases)
- [Supported formats](#supported-formats)
- [How it works](#how-it-works)
- [Architecture](#architecture)
- [Requirements](#requirements)
- [Install, build, test, lint](#install-build-test-lint)
- [Examples](#examples)
- [Usage](#usage)
  - [Full example](#full-example)
  - [In-memory SQLite (single process)](#in-memory-sqlite-single-process)
  - [Server-side setup with PostgreSQL and pgvector](#server-side-setup-with-postgresql-and-pgvector)
  - [Scaling up with HNSW](#scaling-up-with-hnsw)
  - [Choosing an embedder model](#choosing-an-embedder-model)
  - [Optimizing search with tasks](#optimizing-search-with-tasks)
  - [Other search configurations](#other-search-configurations)
  - [Custom AI client](#custom-ai-client)
  - [Delta configurations](#delta-configurations)
- [Documents](#documents)
- [License](#license)

## What semantic search is

Semantic search matches on meaning, not on shared words. A query and a result can rank as a
strong match even when they have no words in common, because the search compares what they mean.

| Search query | Search result |
|---|---|
| *a gift for someone who loves cooking* | The chef's guide to essential kitchen knives |
| *how to feel less tired during the day* | Tips for building a better sleep routine |
| *my plant's leaves are turning yellow* | Common causes of overwatering in houseplants |
| *something fun to do with kids on a rainy day* | Indoor board games for the whole family |
| *ways to stay warm in winter* | A guide to insulated jackets and wool layers |

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

## Architecture

The **Engine** is the single entry point. It runs two flows, indexing files and searching.
Both use the same building blocks. **Strategies** turn files into text chunks. An **embedding
model** and an **AI client** turn text into vectors. Two stores keep the document metadata and
the vectors.

```mermaid
flowchart TD
    App[Your application] --> Engine

    subgraph Engine [Semantic Search Engine Facade]
        Index[Index flow]
        Search[Search flow]
    end

    Index --> Strategies[Strategies<br/>Markdown · PDF · Code · Text · DOCX]
    Search --> Model
    Strategies --> Model[Embedding model<br/>prompt templates]
    Model --> Client[AI client<br/>OpenAI-compatible transport]
    Client --> Server[(Embedding server<br/>LM Studio · Ollama · remote)]

    Index --> Meta[(Metadata store<br/>SQLite · PostgreSQL)]
    Index --> Vectors[(Vector store<br/>sqlite-vec · pgvector)]
    Search --> Vectors
    Search --> Meta

    classDef blue fill:#E6F7FC,stroke:#10C2EB,stroke-width:2px,color:#0A5A72;
    classDef accent fill:#FFF3DC,stroke:#F5A623,stroke-width:2px,color:#8A5410;

    class App,Index,Search,Strategies blue;
    class Model,Client,Server,Meta,Vectors accent;
```

- **Strategies** claim files by type and split them into chunks; the AI client sends each chunk
  to the embedding server and gets back a vector.
- **Indexing** stores the document metadata and the vectors in their two stores.
- **Searching** embeds the query the same way, finds the nearest vectors, and resolves them back
  to their documents through the metadata store.

## Requirements

- **Every use case** needs an **OpenAI-compatible embedding server** (it does not mean actual OpenAI models): on your own machine (LM Studio, Ollama, or llama.cpp), or on a remote host (Google AI Studio or any other server that speaks the standard protocol).
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

## Examples

Runnable programs live in [`examples/`](examples), each a single `main` that indexes the sample
files in [`examples/files`](examples/files) and searches them. They need an OpenAI-compatible
embedding server on `http://127.0.0.1:1234` (e.g. LM Studio) serving EmbeddingGemma.

- **basic**: index into on-disk SQLite and run a search.
- **searchconfig**: tune results with `SearchConfig` (task, minimum relevance, document and chunk limits).
- **postgres**: the server-side setup on PostgreSQL with pgvector.

```sh
git clone https://github.com/DavidBelicza/semantic-search.git
cd semantic-search

go run ./examples/basic
go run ./examples/searchconfig
```

The postgres example needs the bundled database running first:

```sh
docker compose -f test/docker/docker-compose.yml up -d
go run ./examples/postgres
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
	// the engine returns the documents whose meaning is closest to it, each
	// carrying the chunks that matched inside it, so results are matched by
	// meaning rather than exact keywords.
	docs, _ := engine.Search(ctx, semanticsearch.SearchConfig{
		Query: "how do I detect security threats in logs",
	})
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
func (myModel) BuildQuery(query, taskType string) (string, error) { return query, nil }

// semanticsearch.NewEngine(semanticsearch.Config{ Model: myModel{}, ... })
```

### Optimizing search with tasks

Models can search differently depending on the task, and the available tasks depend on the model. The task is an optional last argument to `Search`; leave it out to use the model's default retrieval task. For example, Gemma searches differently based on its task:

```go
semanticsearch.NewModel(semanticsearch.Gemma300mQAT)
...
engine.Search(ctx, semanticsearch.SearchConfig{Query: "I want a spicy tea"})
```

```go
semanticsearch.NewModel(semanticsearch.Gemma300mQAT)
...
engine.Search(ctx, semanticsearch.SearchConfig{
	Query:    "I want a spicy tea",
	TaskType: semanticsearch.TaskGemma.Classification,
})
```

Gemma has 7 tasks. Other models instead take free text as the task. For example:

```go
semanticsearch.NewModel(semanticsearch.Qwen30_6B1024)
...
engine.Search(ctx, semanticsearch.SearchConfig{
	Query:    "I want a spicy tea",
	TaskType: "Find the most exclusive product for this query",
})
```

### Other search configurations

The search config also bounds the results: `MinRelevance` drops weak matches, `MaxDocuments` caps how many documents come back, and `MaxChunks` caps the chunks kept per document.

```go
engine.Search(ctx, semanticsearch.SearchConfig{
	Query:        "I want a spicy tea",
	TaskType:     semanticsearch.TaskGemma.QuestionAnswering,
	MinRelevance: 0.3,
	MaxDocuments: 10,
	MaxChunks:    3,
})
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

### Delta configurations

By default, re-indexing removes documents whose files were deleted from disk, along with their chunks and vectors.

```go
engine.Index(ctx, "path/to/files", semanticsearch.IndexOptions{})
```

Set `KeepMissingFiles` to keep those documents in the index even after their files are gone.

```go
engine.Index(ctx, "path/to/files", semanticsearch.IndexOptions{KeepMissingFiles: true})
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
