# Architecture

A Go library for semantic search over a directory of files (Markdown, PDF, code, DOCX, plain
text). Files are discovered, chunked, and embedded through an OpenAI-compatible model server,
then stored either embedded in SQLite or server-side in PostgreSQL. Search embeds the query,
ranks chunks by vector similarity, and returns the matching documents.

## Layers

```
semanticsearch.go    library facade (package semanticsearch): builds the object graph
                     and exposes Index, Search, and the constructors
internal/pipeline    the flow — indexing and the document search
core/search          public search types (SearchConfig, SearchResult, DocumentResult)
                     and the Searcher seam
core/strategy        the per-file contract (Strategy interface) + Pool; concrete
                     strategies live in subpackages:
                       strategy/general   base structured strategy + chunking engine
                       strategy/markdown  Markdown parsing/chunking
                       strategy/pdf       PDF parsing (PDFium) + font-based sections
                       strategy/code      code parsing (Chroma lexer) + definition sections
                       strategy/docx      DOCX parsing (zip + XML) + heading sections
core/storage         resource entities (Document, Chunk, …); no database code
  storage/sqlite     documents + chunks tables — embedded source of truth
  storage/sqlitevec  sqlite-vec vectors — embedded
  storage/postgres   documents + chunks tables — server-side source of truth
  storage/pgvector   pgvector vectors — server-side
internal/textproc    generic, dependency-free text utilities (split, window, tokens,
                     hash, normalize, part packing, heading-path stack)
internal/fs          stable file identity (device + inode)
core/embedder
  client             the embedding transport client (OpenAI-compatible)
  model              the model definitions (id, dimensions, prompt templates; e.g. Gemma)
```

Rule of thumb: `internal/*` and `core/*` provide the parts; the root `semanticsearch` package
assembles them into an `Engine` that callers drive. Dependencies point downward: `textproc` and
`storage` depend on nothing of ours; strategies depend on both; the pipeline depends on the
strategy contract.

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

Three functions in `internal/pipeline`; they own iteration, database writes, and the status
decisions that advance or stop the flow.

- **Index** — walk the tree, ask the pool which strategy claims each file, register the
  claimed ones, then fingerprint the indexed documents to detect content changes.
- **Process** — for each scanned document, read the bytes and run `Parse → Chunk → Embed`;
  between those it reconciles chunks, writes vectors, and updates status.
- **Cleanup** — page through the stored documents and remove those whose file no longer exists
  (with their chunks and vectors). It runs after indexing, when the walk has refreshed the paths,
  so a moved file is not mistaken for a deleted one; it is skipped when `KeepMissingFiles` is set.

Document status machine: `indexed → scanned → chunked → embedded`. Unchanged files are
short-circuited (fingerprint match) so they are never re-chunked or re-embedded.

### Change detection — two tiers

Deciding what to re-embed is cheap first, accurate second:

1. **Size + mtime, at the upsert.** The walk only stats files; `UpsertDocuments` compares the
   stored `file_size` / `modified_at_ns` against the incoming ones and resets status to
   `indexed` only when either moved. An untouched file keeps its status (`embedded`) and is
   therefore invisible to every later stage — it is never opened. This is what makes a re-scan
   of a large tree fast, and it is why the indexing phase's `done` finishes below its total on
   a re-run: nothing ever visits an untouched file, so nothing can count it.
2. **SHA-256, in fingerprint.** A file the first tier flagged is read and hashed. If the hash
   equals `embedded_content_hash`, the file was merely touched and is restored to `embedded`
   without re-embedding.

The gap: content that changes while size and mtime both stay identical is invisible. That
takes deliberate mtime preservation (`rsync --times`, some restores); `rebuild` is the escape
hatch.

### File identity — what counts as "the same file"

A document is keyed by `FileID` (`internal/fs`), which decides whether a file on disk is one
already in the database. It is platform-dependent:

- **Unix (macOS, Linux)** — device + inode. Identity survives a rename or move: the walk
  refreshes the path on the existing row, size and mtime still match, and the document is left
  untouched. Relocating files costs nothing, which is what the Cleanup note above relies on.
- **Everything else, Windows included** — the absolute path
  ([internal/fs/file_id_other.go](../internal/fs/file_id_other.go)), because Windows does not
  expose device/inode through `os.FileInfo`.

**Accepted behavior:** on Windows a relocated file is a new identity. It is rediscovered and
registered as a new document, fully re-embedded, and the old path — now missing — is deleted
by Cleanup along with its chunks and vectors. The end state is correct (the content is indexed
exactly once, under its new path), but the move costs a full re-embed instead of nothing.
Reorganizing a large corpus on Windows is therefore a long run, not a no-op. Windows does
expose an inode-equivalent (`GetFileInformationByHandle`); it is not used, because it needs
`golang.org/x/sys/windows` and a file handle per file rather than a bare stat.

## Progress reporting

`IndexOptions.OnProgress` reports `(phase, done, total)` as a run advances; it is called
synchronously on the indexing goroutine, so a slow callback slows the run. The counters live
in `internal/pipeline` (`ProgressTracker`), which the facade creates once per run and passes
to all three pipelines.

**The counters never touch the database.** The only total available for free is what the walk
found, counted in memory, so that is what `indexing` reports. Each phase counts its own set,
so `total` is per phase, not per run:

| Phase | Covers | Total | A document is done when |
|---|---|---|---|
| `scanning` | walk + upsert | **0** — unknown until the walk ends | — |
| `indexing` | fingerprint + Process | `len(files)` from the walk | its vectors are stored (`markEmbedded`), or fingerprint finds its content unchanged and skips it |
| `cleanup` | Cleanup | **0** — the stored document set is only known to the database | it has been stat'ed |

The phase boundary does not match the function boundary: `Index` opens `scanning`, then opens
`indexing` itself before calling `fingerprint`, and `Process` keeps counting into the phase
`Index` opened. `Cleanup` opens its own.

### What `done` reaching `total` means

`indexing`'s total is **files considered, not files needing work** — those are different
numbers on a re-run, and the difference is by design:

- **First run** — every file is new, so all of them are worked on and `done` reaches `total`.
- **Re-run** — the upsert leaves untouched files at their existing status (see *Change
  detection*), so they are invisible to `fingerprint` and `Process` and no pipeline ever
  visits them. Nothing can count them. 12 changed files out of 100,000 gives
  `indexing 12 of 100000`.
- **Errors** — a failed document is left for the next run and never counted as finished, so
  `indexing` also ends short when `FailFast` is off.

Reporting files-needing-work instead would mean a `COUNT(*)` on `status`, because the
classification happens inside the upsert's `ON CONFLICT` clause and its result exists only in
the database. That is a deliberate trade: the counters stay free of queries, and `total` means
"files looked at". See [examples/progress](../examples/progress).

## Storage

The resource entities (`Document`, `Chunk`, `ChunkEmbedding`, `FileMetadata`, status
constants) live in `core/storage` and depend on no database, so business logic can
reference the model without coupling to a store. Two backends implement the same interfaces,
and in both the document/chunk metadata and the vectors are **separate stores**:

- **Embedded** — `core/storage/sqlite` holds the `documents` and `chunks` tables (source of
  truth for chunk text); `core/storage/sqlitevec` holds the embeddings in a sqlite-vec `vec0`
  virtual table (`chunk_vectors`). Search is exact KNN over unit-normalized vectors (L2 ranks
  as cosine).
- **Server-side** — `core/storage/postgres` holds the same tables in PostgreSQL;
  `core/storage/pgvector` holds the embeddings in a pgvector table. Search is exact KNN by
  default, or approximate through an opt-in HNSW index.

Because metadata and vectors are separate stores (separate files in the embedded case),
grouping search hits into documents happens in Go, not as a SQL join. See
[research/sqlite-vec-migration.md](research/sqlite-vec-migration.md) and
[research/vector-search-scaling.md](research/vector-search-scaling.md).

## Embedding

Embedding is split into two injected parts: a **model** (`strategy.EmbeddingModel`) that owns the
model id, vector size, and prompt templates, and an **AI client** (`strategy.AiClient`) that
is the transport client speaking the OpenAI-compatible API. Keeping them separate lets the
same client serve any model. The default model is `EmbeddingGemma-300m-qat` (768-dim); its
`BuildData`/`BuildQuery` apply the templates documents and queries need — omitting them badly
degrades ranking.

## Search — query to documents

`Engine.Search` takes a `SearchConfig` and returns `DocumentResult`s, most relevant first, each
carrying the chunks that matched inside it. Chunks stay an internal detail — they are how a
document is embedded and matched, but the caller always gets documents. The flow lives in
`internal/pipeline`, behind the public `search.Searcher` seam so the whole algorithm is
replaceable (inject a `Searcher` through the engine config):

1. **Phrase and embed** the query with the model and AI client.
2. **Fetch** the top `maxSearchChunks` (4096, sqlite-vec's hard limit on KNN `k`) nearest chunk
   hits from the vector store. This is a fixed internal cap, not a caller knob: it bounds the work
   on very large indexes while staying cheap, because this stage carries only chunk ids and scores,
   no text.
3. **Map and filter** — a light `chunk_id → document_id` lookup (no text) resolves each hit to its
   document, and matches below `MinRelevance` are dropped.
4. **Group** the ranked chunks into documents; a document's score is its best (most relevant)
   chunk, and documents come out ordered by that score.
5. **Cap** to `MaxDocuments` documents and `MaxChunks` chunks each.
6. **Hydrate** only the survivors: load text and title for the kept chunks and each document's
   path. Deferring the text load to this final step is what keeps the earlier stages light, so a
   large fetch cap costs little.

Relevance is `1 - distance/2` (both backends use a cosine-equivalent metric with distance in
`[0, 2]`), so scores are 0–1 with higher meaning closer, matching how `MinRelevance` is set.
`SearchConfig` defaults: `MinRelevance` 0 (keep everything), `MaxDocuments` 20, `MaxChunks` 3.

## Build

cgo — both `mattn/go-sqlite3` and the sqlite-vec bindings compile from source, so a C
compiler is required (no prebuilt native libraries or install steps). PDF text extraction
uses PDFium compiled to WebAssembly and embedded in the binary via `go-pdfium`; it needs no
CGO and nothing installed. See
[research/pdf-extraction-engine.md](research/pdf-extraction-engine.md).
