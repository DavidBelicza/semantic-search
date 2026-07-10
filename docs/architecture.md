# Architecture

Local, private semantic search over Markdown and PDF files. Files are discovered, chunked,
embedded with a local model, and stored in SQLite; search is exact (brute-force) vector
similarity. Nothing leaves the machine.

## Layers

```
main.go              thin entry — calls cmd.Execute
cmd/                 CLI (cobra): index, search. Parses input, proxies to pkg.
pkg/                 bootstrapper (package semanticsearch): builds the object graph
                     and runs the pipelines. Public API: Index, Search.
internal/pipeline    the flow — functions that move between files
core/strategy        the per-file contract (Strategy interface) + Pool; concrete
                     strategies live in subpackages:
                       strategy/general   base structured strategy + chunking engine
                       strategy/markdown  Markdown parsing/chunking
                       strategy/pdf       PDF parsing (PDFium) + font-based sections
                       strategy/code      code parsing (Chroma lexer) + definition sections
                       strategy/docx      DOCX parsing (zip + XML) + heading sections
core/storage         resource entities (Document, Chunk, …); no database code
  storage/sqlite     documents + chunks tables (source of truth)
  storage/sqlitevec  sqlite-vec vectors
internal/textproc    generic, dependency-free text utilities (split, window, tokens,
                     hash, normalize, part packing, heading-path stack)
internal/fs          stable file identity (device + inode)
core/embedder        the embedding transport client (OpenAI-compatible) plus the
                     model definitions (id, dimensions, prompt templates; e.g. Gemma)
```

Rule of thumb: `internal/*` provides the parts, `pkg` assembles them, `cmd` exposes them,
`main` starts them. Dependencies point downward: `textproc` and `storage` depend on
nothing of ours; strategies depend on both; the pipeline depends on the strategy contract.

## Strategy — the per-file recipe

A `Strategy` owns the complete life of a single file and does no file I/O or directory
walking (the pipeline hands it the bytes). Interface (`core/strategy`):

```
Claims(path) bool
CreateMetadata(FileRef) (FileMetadata, error)
Fingerprint(content) string
Parse(content) (ParsedDocument, error)        // bytes → sections
Chunk(doc, ParsedDocument) ([]Chunk, error)   // sections → chunks
Embed(ctx, chunks) ([][]float32, error)
```

`Parse` produces a `ParsedDocument` — the file's sections, each a heading path plus body.
`Chunk` slices those sections into chunks, so every chunk carries its heading context.

- **`general.GeneralStrategy`** — the base structured strategy: claims plain-text extensions,
  stat→metadata, hash, one section from the whole text, structured chunking (it owns the
  shared `ChunkSections` engine), embed via the injected embedder.
- **`markdown`** — overrides `Claims` (extension), `Parse` (goldmark headings → sections),
  and `Chunk` (fence-aware).
- **`pdf`** — overrides `Claims` and `Parse` (PDFium extracts font-annotated runs; headings
  are inferred from font size). It inherits chunking, metadata, fingerprint, and embed.
  See [research/pdf-extraction-engine.md](research/pdf-extraction-engine.md).
- **`code`** — overrides `Claims` (code extensions, minus minified/generated files), `Parse`
  (normalize + guard), and `Chunk` (a Chroma lexer finds definition boundaries by token
  category — pure Go, no CGO — one section per function/class, titled with its nesting path).
  See [chunking.md](chunking.md).
- **`docx`** — overrides `Claims` (`.docx`) and `Parse` (unzips the document with the standard
  library — no CGO — and maps Word heading paragraphs onto the heading-path model via
  `outlineLvl`). It inherits chunking, metadata, fingerprint, and embed. See
  [chunking.md](chunking.md).

Markdown, PDF, Code, and DOCX **embed** `GeneralStrategy` (Go composition, not inheritance), reusing its
methods without proxy code and overriding only what their format needs. The embedder is
injected, because embedding is a per-file operation the strategy owns. A `Pool` holds the
strategies; `Pool.For(path)` returns the first that claims a file.

## Pipelines — the flow between files

Two functions in `internal/pipeline`; they own iteration, database writes, and the status
decisions that advance or stop the flow.

- **Index** — walk the tree, ask the pool which strategy claims each file, register the
  claimed ones, then fingerprint the indexed documents to detect content changes.
- **Process** — for each scanned document, read the bytes and run `Parse → Chunk → Embed`;
  between those it reconciles chunks, writes vectors, and updates status.

Document status machine: `indexed → scanned → chunked → embedded`. Unchanged files are
short-circuited (fingerprint match) so they are never re-chunked or re-embedded.

## Storage

The resource entities (`Document`, `Chunk`, `ChunkEmbedding`, `FileMetadata`, status
constants) live in `core/storage` and depend on no database, so business logic can
reference the model without coupling to a store. One SQLite file holds the data:

- `core/storage/sqlite` — `documents` and `chunks` tables. Source of truth (chunk text
  lives here).
- `core/storage/sqlitevec` — a sqlite-vec `vec0` virtual table (`chunk_vectors`) with
  the embeddings. Search is exact KNN over unit-normalized vectors (L2 ranks as cosine).
  See [research/sqlite-vec-migration.md](research/sqlite-vec-migration.md) and
  [research/vector-search-scaling.md](research/vector-search-scaling.md).

## Embedding

Embedding is split into two injected parts: a **model** (`strategy.EmbeddingModel`) that owns the
model id, vector size, and prompt templates, and an **embedder** (`strategy.Embedder`) that
is the transport client speaking the OpenAI-compatible API. Keeping them separate lets the
same client serve any model. The default model is `EmbeddingGemma-300m-qat` (768-dim); its
`BuildData`/`BuildQuery` apply the templates documents and queries need — omitting them badly
degrades ranking.

## Build

cgo — both `mattn/go-sqlite3` and the sqlite-vec bindings compile from source, so a C
compiler is required (no prebuilt native libraries or install steps). PDF text extraction
uses PDFium compiled to WebAssembly and embedded in the binary via `go-pdfium`; it needs no
CGO and nothing installed. See
[research/pdf-extraction-engine.md](research/pdf-extraction-engine.md).
