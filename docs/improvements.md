# Improvements Backlog

Issues found in the current implementation, to be worked through. The
**Deferred — intentionally not now** section below lists what is intentionally left
alone for this round; everything else is fair game.

The canonical design reference is
[go-vector-indexer-implementation.md](go-vector-indexer-implementation.md); section
numbers below (§) refer to it. Project guidance lives in
[CLAUDE.md](../CLAUDE.md) (notably the flat-code-structure rules).

---

## Done since the initial backlog

- **Structure-aware chunking — ✅ done.** The hard-cut placeholder was replaced with a
  goldmark-based Markdown chunker (heading sections, ~350-token budget, overlap,
  heading-path/note-name titles, oversized-block splitting). See
  [chunking-design.md](chunking-design.md). `HardLimitChunker` remains for tests.
- **Parser normalization — ✅ done.** `MarkdownParser`
  ([internal/parser/markdown.go](../internal/parser/markdown.go)) now strips the BOM,
  normalizes line endings, collapses blank-line runs, and trims edges. Image/embed
  references are kept.

## Deferred — intentionally not now

- **Exact token counting / safety margin** — the chunker estimates tokens as ~`chars/4`
  (`EstimateTokenCount`) with no model tokenizer. Fine at a 350-token budget (well under
  the 2048 model limit), so low priority. A pure-Go option (`sugarme/tokenizer` loading
  the model's `tokenizer.json`) is in reserve. (§10.4)
- **Embedding request batching** — sending ≤16 chunks per HTTP request (§11.5) is
  left out for now; measured batching gave little gain on this hardware anyway (see
  [research-vector-search-scaling.md](research-vector-search-scaling.md)).
- **File rename does not re-embed** — a headingless note's title derives from its
  filename, but renaming the file isn't a content change, so the content-hash skip
  won't re-chunk it and the stored title/embedding goes stale until the content
  changes. Edge case; low priority.

---

## Embedding

> **Status: E1–E5 done.** The embedder
> ([internal/embedder/openai.go](../internal/embedder/openai.go)) now uses a
> timeout client, retries transient failures with capped exponential backoff,
> checks HTTP status before decoding, validates returned dimensions at the boundary,
> and has tests covering retry/no-retry, dimension mismatch, and JSON
> marshaling/escaping round-trip.

### E1. HTTP client has no timeout — ✅ done
~~`NewOpenAIEmbedder` uses `http.DefaultClient`, which has `Timeout: 0`.~~ Fixed:
the constructor builds an `http.Client{Timeout: DefaultRequestTimeout}` (60s) and
requests honor context cancellation. (§11.6)

### E2. No retries / backoff — ✅ done
~~No retry on transient errors or 429/5xx; no backoff; no cap.~~ Fixed:
`embedWithRetry` retries up to `MaxRetries` (default 3) on transient network errors,
429, and 5xx, with capped exponential backoff (`backoffDelay`, ctx-aware
`sleepBackoff`). 4xx and dimension mismatches are **not** retried. (§11.6)

### E3. Error/status check ordering masks real failures — ✅ done
~~The response body is JSON-decoded before the HTTP status is checked.~~ Fixed:
`embedOnce` reads the body, then checks status first; non-2xx returns
`responseError` (status + provider message / raw body) without attempting to parse
embeddings. (§11.6)

### E4. Verify JSON request marshaling/escaping is correct — ✅ done
`TestEncodeEmbeddingRequestRoundTripsAndKeepsRawHTML` round-trips representative
chunks (newlines, UTF-8 `§`/`'`/`%C3%A9`, raw `&<>` in URLs, code fences/backticks)
and asserts valid JSON, byte-identical decode (no double-escaping/mojibake), and
that `SetEscapeHTML(false)` keeps `&<>` raw (no `\u00XX`). (§11.1)

### E6. Embedding model id and task prefixes — ✅ done (relevance fix)
Search returned irrelevant, tiny results. Root cause was **not** the distance metric
(the model already returns unit vectors, so L2 is cosine-equivalent — confirmed by
inspecting stored norms = 1.0). Two real causes:
- The default model id was the placeholder `text-embedding-model`, which LM Studio
  does not recognize (returns HTTP 400). Now defaults to the real
  `text-embedding-embeddinggemma-300m-qat`.
- The model requires prompt templates. Current default is **EmbeddingGemma**:
  documents are formatted per-chunk by `embedder.DocumentInput` as
  `title: <heading path or note name> | text: <body>`, and queries use `QueryPrefix`
  (`task: search result | query:`) in [cmd/search.go](../cmd/search.go). The chunk
  title lives in `chunks.title`; stored chunk text is the raw body.
  (A `nomic-embed` setup with `search_document:` / `search_query:` prefixes was the
  earlier default and also works via the `Prefix` field.)

After re-embedding (`rebuild` or a reset + re-index), searches return relevant chunks
with their heading-path/note-name title shown.

**Follow-up:** the model id, base URL, dimensions, and prompt templates are hardcoded.
They should become `--embedding-model` / `--embedding-url` flags (§5) so switching
models (which need different templates) works without code changes. Changing the model
or its templates requires a full re-embed.

### E5. Configured-dimension validation happens too late — ✅ done
~~The embedder never validated returned vectors against the configured dimension.~~
Fixed: `OpenAIEmbedder.Dimensions` (set to `DefaultDimensions` in
[strategy.DefaultPool](../internal/strategy/default.go)) is enforced by
`validateDimensions` right after parsing, returning a clear
`"embedding dimension mismatch"` error before the vectors ever reach LanceDB.
(§11.4)

---

## Pipeline logic

### L1. Wasteful full re-embed on a metadata-only change — ✅ done
~~`touch`-ing a file (mtime changes, content identical) re-staged it to `scanned`,
and the old "reload ALL chunks" branch re-embedded the whole document even though
valid vectors already existed.~~ Fixed via a new `documents.embedded_content_hash`
column that records the content hash that has been fully embedded:

- `chunksForEmbedding` ([internal/strategy/default.go](../internal/strategy/default.go))
  embeds **only newly inserted chunks** when the document was already embedded
  (`embedded_content_hash` set) — so an unchanged `touch` re-embeds nothing — and
  embeds **all** current chunks only when the document was never embedded (covering
  the rare "chunks exist without vectors" case that the old reload-all branch was
  really guarding).
- `MarkDocumentEmbedded` records `embedded_content_hash = content_hash` when a
  document reaches `embedded`, used by both the scanned and chunked stages.
- Migration: `ensureEmbeddedContentHashColumn` adds the column to existing
  databases (the repo's `vector-index.db` predates it); covered by
  `TestEnsureSchemaAddsEmbeddedContentHashColumnToLegacyDatabase`.
- The scanner short-circuits at the hash check
  ([internal/scanner/scanner.go](../internal/scanner/scanner.go)): when the freshly
  computed content hash equals `embedded_content_hash`, the document is restored
  straight to `embedded`, so a `touch` skips re-chunking **and** re-embedding — the
  file is read exactly once (to hash it). Verified by
  `TestScanIndexedDocumentsRestoresEmbeddedWhenContentMatchesEmbeddedHash`.
- Behavior verified by
  `TestProcessScannedDocumentsSkipsReembeddingWhenAlreadyEmbeddedAndUnchanged`.

Fully realizes §8.3 step 4 ("if the hash is unchanged, update metadata but do not
re-embed"). A pure size/length check was rejected as unsafe — a same-size edit would
be missed — so the content hash (read once) remains the source of truth.

### L2. Always fail-fast — one embed failure aborts the whole run — ✅ done
~~An error propagated up through `Index` and stopped all remaining documents.~~ The
scan, scanned-processing, and chunked-processing passes now share
`processDocumentsByStatus` ([internal/strategy/default.go](../internal/strategy/default.go),
[internal/scanner/scanner.go](../internal/scanner/scanner.go)), which takes a
`failFast` flag: when unset, a per-document error is collected and processing
continues, and the joined errors are returned at the end; when set, the run stops on
the first error. The loops paginate by ascending document id, so a failed (and
therefore still-queued) document is not revisited within the run. `--fail-fast` is
exposed on the `index` and `scan` commands. (§17)

### L3. Cross-store ordering isn't crash-safe; no rebuild path — ✅ done
- **Ordering:** `processScannedDocument` now applies the SQLite chunk reconcile
  (the source of truth) and commits **before** deleting vectors from LanceDB
  ([internal/strategy/default.go](../internal/strategy/default.go)), so a crash can
  no longer leave committed chunk rows pointing at vectors that were already deleted.
- **Rebuild path:** `strategy.RebuildVectors` / `semanticsearch.Rebuild` re-embed
  every embedded document's chunks and replace their vectors, rebuilding the LanceDB
  index from SQLite. Exposed as a new `rebuild` command. Recreates a lost or drifted
  vector store from the source of truth. (§12.2)

### L4. `UNIQUE(document_id, chunk_index)` violated mid-transaction on reorder — ✅ done
~~Sequential `UPDATE chunks SET chunk_index = ...` for kept chunks could collide with
a not-yet-updated row when two chunks swap positions.~~ Fixed:
`moveKeptChunksToTemporaryIndexes`
([internal/storage/sqlite/storage.go](../internal/storage/sqlite/storage.go)) first
moves every kept chunk to a unique negative temporary index, then applies the final
indices, so no transient collision is possible. Verified by
`TestApplyDocumentChunkReconcileSwapsKeptChunkIndexesWithoutUniqueViolation`.

---

## Cross-platform & crawler

### C1. Indexing is broken on Windows despite Windows build support — ✅ done
~~`fileIDFromInfo` type-asserted `info.Sys().(*syscall.Stat_t)`, which does not exist
on Windows, so the package would not even compile there.~~ `fileID` is now split by
build tag: [file_id_unix.go](../internal/crawler/file_id_unix.go) uses the
device/inode identity on Unix, and [file_id_other.go](../internal/crawler/file_id_other.go)
falls back to the absolute path elsewhere. `crawler.go` no longer references
`syscall`; verified by cross-compiling the package for `GOOS=windows`. (§8.2)

### C2. Crawler ignores all skip rules — ✅ done
`CollectFileMetadata` now takes `crawler.Options{IncludeHidden, FollowSymlinks}` and
filters during the walk: it `SkipDir`s `.git`, `.hg`, `.svn`, `node_modules`,
`vendor`, `dist`, `build`, `.cache`, `.Trash`, skips hidden files and directories
unless `IncludeHidden`, and follows symlinks only when `FollowSymlinks`. Wired to the
`--include-hidden` / `--follow-symlinks` flags on the `index` command. Verified by
`TestCollectFileMetadataSkipsVcsBuildAndHiddenByDefault`. (§8.1)

---

## Minor / latent

### M1. Rune offsets stored as offsets
`StartOffset`/`EndOffset` are `[]rune` indices
([internal/chunker/hard_limit.go:57](../internal/chunker/hard_limit.go)). Harmless
today (nothing reads them) but wrong if later used as byte offsets into the file for
non-ASCII content. Decide on byte vs. rune semantics and document it.

### M2. SQLite has no `busy_timeout` / WAL / connection cap — see multi-command note
`Open` leaves the default connection pool
([internal/storage/sqlite/storage.go:63](../internal/storage/sqlite/storage.go)). Safe
while the pipeline is strictly sequential (batch size 1, no goroutines), but it will
bite as soon as concurrency or multiple processes touch the same database. **This is
the point most affected by the multi-command direction below.**

### M3. `ProcessChunkedDocuments` is near-redundant within a single run
`ProcessScannedDocuments` already drives docs all the way to `embedded`, so within
one `Index` invocation the chunked-retry pass
([pkg/workflows.go:51](../pkg/workflows.go)) only does work across *separate*
invocations. Keep it (it is the cross-run retry path) but the redundancy is worth a
comment so it isn't mistaken for dead code.

---

## Architecture candidate: single-store vectors (sqlite-vec / vectorlite)

Not scheduled — recorded so the trade-off is on the record.

The project uses **two stores**: SQLite for metadata + a separate **LanceDB** table for
vectors. The design spec mandated this
([go-vector-indexer-implementation.md](go-vector-indexer-implementation.md) §6, and
§28: "Do not model LanceDB as a SQLite extension" / "Do not substitute `sqlite-vec` …
unless explicitly requested"). A vestigial `VECTOR_CLI_VECTORLITE_PATH` env var (§5)
shows a single-store approach was considered early but not taken.

An alternative is a **SQLite vector extension** (`sqlite-vec`, `vectorlite`) that keeps
vectors *inside* SQLite:

- **Pros:** one file, one transaction → vectors + metadata are atomic, which
  **eliminates the entire L3 cross-store consistency problem** and the rebuild path;
  vector search + metadata `JOIN` in one SQL query; HNSW ANN built in (better than our
  current brute force).
- **Cons:** still a per-platform **native dependency** (a loadable SQLite extension, so
  the cgo-style packaging pain does not disappear); newer/smaller ecosystem; weaker at
  the very large (millions–billions) scale where LanceDB's columnar format and index
  options (IVF_PQ) shine.

For a local, single-machine tool at small-to-moderate scale this is arguably the
*simpler and more robust* architecture. The storage layer is already isolated behind
interfaces (`internal/storage/lancedb`), so swapping backends is contained — mostly a
rewrite of that package plus dropping the cross-store staging. Revisit if the two-store
consistency burden or the lack of ANN becomes a real pain point.

---

## Note: multiple CLI commands / concurrent invocations

This is a local CLI. We may later run multiple CLI commands — possibly more than one
process against the same database — though it's not yet certain that model will be
used. If it is, the following become real correctness issues rather than latent ones:

- **M2 (SQLite locking):** two processes writing the same SQLite file will hit
  `SQLITE_BUSY` without a `busy_timeout`. WAL mode + a busy timeout + capping writers
  to one connection per process would be the minimum. Cross-process write
  serialization (e.g. a file lock) may also be needed.
- **LanceDB concurrent access:** a second process mutating the same LanceDB table
  (delete/replace) while another reads/writes has no coordination today.
- **L3 (cross-store consistency):** concurrent processes widen the window where
  SQLite and LanceDB can diverge, making the rebuild/repair path (L3) more important.

Decision pending: confirm whether concurrent multi-command execution is a supported
use case before investing in cross-process locking. If it stays single-process /
one-command-at-a-time, M2 and the items above remain low priority.
