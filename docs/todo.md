# Format support — roadmap

Planned and completed file-format strategies. Each format is a strategy subpackage under
`core/strategy/` that embeds `GeneralStrategy` and overrides only what it needs
(`Claims`, `Parse`, and `Chunk` when the format chunks differently). Adding one touches no
other layer — see [architecture.md](architecture.md).

Priority: **P1** next up, **P2** after, **P3** when the corpus needs it.
Status: **done** / **todo** (partial = base exists, needs wiring).

| Strategy | Extensions | Priority | Status | Go library candidates |
|---|---|---|---|---|
| Markdown | `.md`, `.markdown`, `.mdown` | — | done | `github.com/yuin/goldmark` (in use) |
| PDF | `.pdf` | — | done | `github.com/klippa-app/go-pdfium` (in use) |
| General / plain text | `.txt`, `.text`, `.log`, `.rst`, `.org`, `.adoc` | — | done | stdlib only (base strategy; plain-text `Claims` set, pool wiring, shared `textproc.NormalizeText`) |
| Code | `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.py`, `.php`, `.java`, `.rb`, `.rs`, `.c`, `.h`, `.cpp`, `.hpp`, `.cs`, `.sh`, `.sql` | — | done | `github.com/alecthomas/chroma/v2` lexer (pure Go); structure-aware for brace + indent families; Ruby/SQL flat-windowed pending own splitter |
| DOCX | `.docx` | — | done | stdlib `archive/zip` + `encoding/xml` (heading sections via `outlineLvl`; tables linearized) |
| Config | `.json`, `.yaml`, `.yml`, `.toml`, `.ini`, `.properties` | P2 | todo | stdlib (v1, text); `gopkg.in/yaml.v3`, `encoding/json` (optional, key-path structure) |
| HTML | `.html`, `.htm` | P2 | todo | `golang.org/x/net/html`; alt `github.com/PuerkitoBio/goquery` |
| CSV / TSV | `.csv`, `.tsv` | P2 | todo | stdlib `encoding/csv` |
| XLSX | `.xlsx` | P3 | todo | `github.com/xuri/excelize/v2` (BSD); or stdlib `archive/zip` + `encoding/xml` |
| EPUB | `.epub` | P3 | todo | stdlib `archive/zip` + `golang.org/x/net/html`; alt `github.com/taylorskalyo/goreader` |
| Subtitles | `.srt`, `.vtt`, `.ass`, `.ssa` | P3 | todo | stdlib (`.srt`/`.vtt`); `github.com/asticode/go-astisub` (`.ssa`/`.ass`/`.ttml` too) |

## Out of scope (for now)

- **Legacy binary office** (`.doc`, `.xls`, `.ppt`) — OLE binary, no good pure-Go reader.
  Convert upstream to the Open XML form, or skip.
- **Images and scanned PDFs** — need OCR (e.g. Tesseract): a real feature with a heavy
  dependency and variable quality, not a format add.
- **Audio / video** — transcription; out of scope for a file indexer.

# Document retrieval — search returns documents

Improve retrieval so search always returns **documents**, not chunks. Chunks stay an internal
detail (they are how a document is embedded and matched); the caller always gets document
objects, each carrying the chunks that matched inside it. This mirrors how managed search
products (e.g. Vertex AI Search) return documents with their relevant passages.

**Out of scope:** the vector algorithm (exact KNN vs. HNSW) is *not* part of this work.
Grouping chunks into documents is a plain post-search step over the ranked chunk hits and is
identical for both backends; accuracy (exact on KNN, recall-bounded on HNSW) is a property of the
chosen backend, decided elsewhere.

## API shape

One facade function, one config struct, a document return type:

```go
func (e *Engine) Search(ctx context.Context, query string, config SearchConfig) ([]DocumentResult, error)
```

- `SearchConfig` (public; carries the tunable knobs, all optional):
  - `TaskType string` — model query task (replaces the old variadic `taskType` argument; empty =
    the model's default retrieval task).
  - `ScoreThreshold float64` — the **primary relevance filter**: keep only chunks whose match is
    within this cutoff, so a document appears only if it has a chunk relevant enough. Retrieval
    walks the ranked chunks and stops once matches fall past the threshold. **Caveat:** a raw
    distance value is model-specific (distance scales differ per model/metric), so a threshold
    tuned for one model does not transfer to another. Prefer exposing it as a normalized relevance
    (0–1, e.g. from cosine) rather than raw distance so it is portable and intuitive — decide the
    exact form at implementation.
  - `DocumentLimit int` — a **hard cap on documents returned**, the safety valve so a loose
    threshold on a large corpus does not return the whole database. When set, retrieval also stops
    once N distinct documents are collected.
  - Bounding rule: retrieval stops at the first of — matches exceeding `ScoreThreshold`, or
    `DocumentLimit` distinct documents collected. At least one must be effective, so when both are
    unset apply a sane default `DocumentLimit` rather than returning the entire store. There is no
    caller-facing chunk-count knob; any chunk batching is an internal detail.
- `DocumentResult` (public; the only thing Search returns):
  - `DocumentID int64`, `AbsolutePath string` (document identity), `Score float64` (best/among
    its chunks — the minimum chunk distance), and `Chunks []SearchResult` (the matching chunks,
    ranked). `SearchResult` stays as the per-chunk evidence type, no longer a top-level return.

## Search interface + one implementation

Introduce a single search seam so the whole flow is replaceable later, with one implementation now:

```go
type Searcher interface {
    Search(ctx context.Context, query string, config SearchConfig) ([]DocumentResult, error)
}
```

- One implementation (e.g. `documentSearcher`) holds the dependencies (metadata store, vector
  store, model, AI client) and does the flow below.
- The engine builds it in `NewEngine` from its existing dependencies and the facade `Search`
  delegates to it. (Promoting `Searcher` to a public package for caller-supplied implementations
  is a later option; for now it is the internal seam and `SearchConfig` is the user-facing knob.)

## Flow (the one implementation)

1. `model.BuildQuery(query, config.TaskType)` → embed the query with the AI client.
2. Retrieve chunk hits ordered by distance, stopping at the first of: a hit past
   `config.ScoreThreshold`, or `config.DocumentLimit` distinct documents collected. Because hits
   are ordered, once one exceeds the threshold every later one does too, so the walk can stop
   early. The store returns ranked chunks (see the threshold retrieval change below); the searcher
   may pull them in growing batches rather than all at once.
3. `ChunkMetadataByIDs` → each hit's `DocumentID`, `Title`, `Text`.
4. Group hits by `DocumentID`, preserving rank; score each document by its best (minimum-distance)
   chunk; order documents by that score.
5. Apply the `DocumentLimit` cap if set.
6. `DocumentsByIDs` (new store method) → the surviving documents' identity (path).
7. Assemble `[]DocumentResult`, each with its ranked `Chunks`.

## Supporting changes

- **Threshold retrieval on the vector store** — today `VectorStorage.Search(vec, limit)` is
  top-k only. Add threshold-aware retrieval (return chunks within a relevance/distance cutoff,
  ordered by distance, optionally with an internal max as a guard). Implement it for the exact
  backends — **sqlite-vec** (always exact, brute-force) and **Postgres KNN** (sequential scan) —
  where a `WHERE distance <= T ORDER BY distance` is natural and exact. On **Postgres HNSW** (the
  only approximate backend, and opt-in) a range/threshold query does not map onto the top-k index,
  so it degrades to top-k-then-filter and is recall-bounded; document that and leave it as-is
  (HNSW is out of scope). sqlite-vec has no HNSW at all, so the default embedded path is fully
  exact.
- **New store method** `DocumentsByIDs(ctx, ids) ([]storage.Document, error)` on `Storage`,
  implemented for both the SQLite and Postgres stores, plus a matching subset on the pipeline's
  `SearchStore` interface. (Metadata and vectors are separate stores — separate files in SQLite —
  so grouping is done in Go over the ranked hits, not as a SQL join.)
- **Type moves/aliases** — `DocumentResult` and `SearchConfig` defined in `internal/pipeline` and
  aliased on the facade, matching how `SearchResult` is already aliased.
- **Update callers** — this changes `Search`'s signature and return type: update
  `semanticsearch_test.go`, the e2e test, and the README search examples (including the
  "Optimizing search with tasks" section, which now passes `SearchConfig{TaskType: ...}`).

## Notes / caveats

- **Backends and exactness:** sqlite-vec (default embedded path) and Postgres KNN are exact, so
  threshold retrieval and document ranking are exact there — no caveats. HNSW exists only as the
  opt-in `PostgresHNSW`; there the threshold walk is recall-bounded (it sees only the index's
  candidates). That is the backend's property, not this feature's, and HNSW is out of scope — just
  document it.
- **Threshold portability:** distance scales are model-specific, so the same raw `ScoreThreshold`
  behaves differently per model. Exposing a normalized relevance (0–1) mitigates this; a wrong
  threshold silently returns too much or too little, so document how to choose it.
- **Cost:** a very loose threshold on a large corpus can scan many chunks. `DocumentLimit` bounds
  the returned set; the internal batched fetch and early stop bound the work. The store returns
  ranked chunks and the searcher does the document walk in Go (metadata and vectors are separate
  stores).
- **Scoring:** "document = its best chunk" (minimum distance). Other aggregations (mean, sum,
  count-weighted) are possible later but out of scope for the first version.

## Examples directory

Create an `examples/` directory at the repository root with small, runnable Go files (each a
self-contained `main`) that showcase the common scenarios end to end, so users can copy a working
setup instead of assembling it from the README:

- **Document search** — the new `Search` returning `DocumentResult`s, with a `SearchConfig`
  (task, score threshold, document limit).
- **Different databases** — embedded SQLite (on-disk), in-memory SQLite, and PostgreSQL + pgvector
  (KNN and HNSW wiring).
- **Different models** — a predefined model (Gemma), another predefined model (e.g. Nomic), a
  template-free `NewGeneralModel`, and a custom `EmbeddingModel` implementation.
- **Setting a task** — passing a `TaskType` (predefined constant and free text).
- **Custom AI client** — injecting a hand-written `AiClient` implementation.

Keep each example minimal and focused on one axis; shared boilerplate can be duplicated for
clarity rather than factored into a helper (these are teaching files, not a library).
