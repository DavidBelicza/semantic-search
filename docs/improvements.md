# Improvements Backlog

Issues found in the current implementation, to be worked through. The
**Deferred — intentionally not now** section below lists what is intentionally left
alone for this round (chunking, parser normalization, batching); everything else is
fair game.

The canonical design reference is
[go-vector-indexer-implementation.md](go-vector-indexer-implementation.md); section
numbers below (§) refer to it. Project guidance lives in
[CLAUDE.md](../CLAUDE.md) (notably the flat-code-structure rules).

---

## Deferred — intentionally not now

Known and deliberately left alone for this round. Listed so they aren't
re-discovered later as "new."

- **Chunking** — hard rune-budget cut (no Markdown-aware boundaries, no overlap, no
  heading paths). Deliberate placeholder.
- **Token safety margin / `chars/4` underestimate** *(was E4)* — the chunker cuts at
  `maxTokens * 4` runes and reports `runes/4` tokens
  ([internal/chunker/hard_limit.go:50](../internal/chunker/hard_limit.go)). Markdown
  and code tokenize denser than 4 chars/token, so real token counts can exceed the
  model's limit and get truncated server-side. This is a chunker concern, so it rides
  with the deferred chunking work. (§10.4)
- **Parser normalization / cleanup before chunking** — the parser is a passthrough
  ([internal/parser/markdown.go](../internal/parser/markdown.go)); trimming
  trailing blank lines, collapsing blank runs, BOM removal, and dropping low-signal
  fragments would live here (§9). Deferred. **Decision so far: keep image references**
  (`![[file.pdf]]`, `![](url)`) — the filenames can carry signal, so do not strip
  them. This is also where the low-signal / empty / URL-shred chunks observed in the
  LM Studio logs would be cleaned up.
- **Embedding request batching** — sending ≤16 chunks per HTTP request (§11.5) is
  left out for now; see the multi-command note at the end.

---

## Embedding

> **Status: E1–E5 done** (not yet committed). The embedder
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

### L2. Always fail-fast — one embed failure aborts the whole run
If `embedChunks` errors, the error propagates up through `Index` and stops all
remaining documents ([pkg/workflows.go:47](../pkg/workflows.go)). The affected doc is
left `chunked` for a later retry, but the current run abandons everything else. Spec
§17 wants unrelated documents to keep processing unless an explicit `--fail-fast` is
set. Add per-document error collection and a fail-fast flag.

### L3. Cross-store ordering isn't crash-safe; no rebuild path
LanceDB delete ([internal/strategy/default.go:208](../internal/strategy/default.go))
runs before the SQLite chunk reconcile commits, and embedding runs after. A crash in
between can leave SQLite and LanceDB inconsistent (chunk rows pointing at deleted
vectors). The design treats SQLite as source of truth and LanceDB as a rebuildable
derived index (§12.2), but nothing actually rebuilds LanceDB from SQLite. Add a
rebuild/repair path and tighten the operation ordering.

### L4. `UNIQUE(document_id, chunk_index)` can be violated mid-transaction on reorder
*(latent / lower confidence)* `ApplyDocumentChunkReconcile` issues sequential
`UPDATE chunks SET chunk_index = ...` for kept chunks
([internal/storage/sqlite/storage.go:387](../internal/storage/sqlite/storage.go)). If
two kept (same-hash) chunks swap positions, one UPDATE can collide with a
not-yet-updated row's index and trip the UNIQUE constraint, aborting that document.
Unlikely with hard-cut chunking (content shifts change hashes) but real for
duplicated/moved identical blocks. Stage index updates to avoid transient
collisions (e.g. two-phase, or offset then settle).

---

## Cross-platform & crawler

### C1. Indexing is broken on Windows despite Windows build support
`fileIDFromInfo` type-asserts `info.Sys().(*syscall.Stat_t)`
([internal/crawler/crawler.go:65](../internal/crawler/crawler.go)), which does not
exist on Windows, so the entire crawl errors out — even though
[link.go](../internal/storage/lancedb/link.go) and the install script ship Windows
libs. Add a fallback file-identity scheme (§8.2 says "when available").

### C2. Crawler ignores all skip rules
`CollectFileMetadata` walks and `Stat`s *every* file, including `.git`,
`node_modules`, `vendor`, and hidden files
([internal/crawler/crawler.go:25](../internal/crawler/crawler.go)); extension
filtering happens only afterward. Not incorrect, but wasteful on real repos, and the
spec's skip-dir / hidden-file / symlink behavior and the `--include-hidden` /
`--follow-symlinks` flags (§8.1) are absent. Filter directories during the walk.

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
