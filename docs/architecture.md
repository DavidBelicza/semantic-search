# Architecture

Local, private semantic search over Markdown files. Files are discovered, chunked,
embedded with a local model, and stored in SQLite; search is exact (brute-force) vector
similarity. Nothing leaves the machine.

## Layers

```
main.go            thin entry — calls cmd.Execute
cmd/               CLI (cobra): index, search. Parses input, proxies to pkg.
pkg/               bootstrapper (package semanticsearch): builds the object graph
                   and runs the pipelines. Public API: Index, Search.
internal/pipeline  the flow — functions that move between files
internal/strategy  the per-file recipe — everything done to one file
internal/storage   sqlite (documents+chunks) and sqlitevec (vectors)
internal/textproc  generic text utilities (split, window, tokens, hash, normalize)
internal/embedder  the embedding client (EmbeddingGemma via LM Studio)
```

Rule of thumb: `internal/*` provides the parts, `pkg` assembles them, `cmd` exposes them,
`main` starts them.

## Strategy — the per-file recipe

A `Strategy` owns the complete life of a single file and does no file I/O or directory
walking (the pipeline hands it the bytes). Interface:

```
Claims(path) bool
CreateMetadata(FileRef) (FileMetadata, error)
Fingerprint(content) string
Parse(content) (text, error)
Chunk(doc, text) ([]Chunk, error)
Embed(ctx, chunks) ([][]float32, error)
```

- `GeneralStrategy` — format-agnostic behaviour (claims everything, stat→metadata, hash,
  bytes→text, fixed-window chunks, embed via the injected embedder).
- `markdownStrategy` — composes `GeneralStrategy` and overrides only the Markdown-specific
  steps: `Claims` (by extension), `Parse` (normalization), `Chunk` (see
  [chunking.md](chunking.md)). It proxies the rest to `GeneralStrategy`.
- The embedder is injected into the strategy, because embedding is a per-file operation.

A `Pool` holds the strategies; `Pool.For(path)` returns the first that claims a file.

## Pipelines — the flow between files

Two functions in `internal/pipeline`; they own iteration, database writes, and the
status decisions that advance or stop the flow.

- **Index** — walk the tree, ask the pool which strategy claims each file, register the
  claimed ones, then fingerprint the indexed documents to detect content changes.
- **Process** — for each scanned document, read the bytes and run `Parse → Chunk →
  Embed`; between those it reconciles chunks, writes vectors, and updates status.

Document status machine: `indexed → scanned → chunked → embedded`. Unchanged files are
short-circuited (fingerprint match) so they are never re-chunked or re-embedded.

## Storage

One SQLite file holds everything:

- `internal/storage/sqlite` — `documents` and `chunks` tables. Source of truth (chunk
  text lives here).
- `internal/storage/sqlitevec` — a sqlite-vec `vec0` virtual table (`chunk_vectors`)
  holding the embeddings. Search is exact KNN over unit-normalized vectors (L2 ranks as
  cosine). See [research/sqlite-vec-migration.md](research/sqlite-vec-migration.md) and
  [research/vector-search-scaling.md](research/vector-search-scaling.md).

## Embedding

`EmbeddingGemma-300m-qat` (768-dim) served over LM Studio's OpenAI-compatible API.
Documents and queries use the model's prompt templates; omitting them badly degrades
ranking.

## Build

cgo — both `mattn/go-sqlite3` and the sqlite-vec bindings compile from source, so a C
compiler is required. No prebuilt native libraries or install steps.
