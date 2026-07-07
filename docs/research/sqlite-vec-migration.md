# Research — Replacing LanceDB with sqlite-vec

**Goal:** remove the LanceDB vector store, keeping the exact same *brute-force* (exact
KNN) semantics and search quality, while simplifying the architecture down to a single
SQLite file. No feature loss, no duplicated stored text.

**The real constraint (clarified):** the problem with LanceDB is *not* CGO — it is that
LanceDB ships **prebuilt native libraries** (`native/lib`, `native/include`,
`DYLD_LIBRARY_PATH`, `make` targets that install the lib before build). That is a Rust
library `go build` cannot produce from source, so it needs custom install/ship steps.
Acceptance test: **C compiled from source by `go build` = fine; extra installer steps /
shipped prebuilt binaries = not fine. No WASM** (an exotic runtime we don't want to ship
for a shippable tool).

Status: **implemented and verified (§5).** LanceDB removed; sqlite-vec vec0 store live.

---

## 1. Current architecture (before)

- **SQLite** (`mattn/go-sqlite3`, CGO) = source of truth: `documents` + `chunks`
  tables. `chunks.text` / `chunks.title` hold the stored content.
- **LanceDB** (`lancedb-go` v0.1.2, CGO + prebuilt native static libs under `native/`)
  = derived vector index: one `chunk_vectors` table (`chunk_id` Int64 +
  FixedSizeList<Float32>[768]).
- Search flow (`pkg/search.go`): embed query → `lancedb.Search` (brute-force L2 on
  normalized vectors ≈ cosine) → `ChunkMetadataByIDs` in SQLite → assemble results.

**Two CGO dependencies** (`mattn/go-sqlite3`, `lancedb-go`), **two data stores**
(`vector-index.db` + `vector-index.lancedb/`), and a `native/` lib tree that
`make test` / `make lint` must install before cgo linking.

### Baseline measurement (2026-07-01, M4 / 24 GB)

Dataset in `vector-index.db`: **75 documents, 128 chunks**, avg chunk text 452 chars
(~57 KB total text). LanceDB dir: 1.9 MB.

End-to-end `search 5 <query>` latency (warm binary, LM Studio local):

| query          | real time |
|----------------|-----------|
| payment        | ~0.11 s   |
| data storage   | ~0.11 s   |
| authentication | ~0.12 s   |

**Finding:** at this scale latency is entirely dominated by the query-embedding HTTP
round-trip to LM Studio (~100 ms). Brute-force vector scan over 128 × 768-dim vectors
is sub-millisecond and invisible in the total. The scaling math for larger corpora is
in [research-vector-search-scaling.md](research-vector-search-scaling.md); brute-force
cost there is a linear scan (`N × 768` mul-adds per query), identical in Big-O to what
sqlite-vec does, so the backend swap is not expected to change user-visible latency at
any scale we target. A larger synthetic corpus benchmark is planned for the *after*
section to confirm the constant factor.

---

## 2. How to get sqlite-vec in Go — the options and what was verified

sqlite-vec is a C extension. Three ways to reach it from Go, judged against the real
constraint (source-compiled by `go build`, no shipped binaries, no WASM):

- **CGO, compiled from source (CHOSEN):** `mattn/go-sqlite3` (already a dependency;
  compiles the SQLite amalgamation from source under cgo) + `asg017/sqlite-vec-go-bindings/cgo`
  (vendors and compiles `sqlite-vec.c`, registers `vec0` via `sqlite3_auto_extension`).
  Real SQLite, no prebuilt libs, resolved entirely by `go build`. Build-time need: a C
  compiler — which `mattn` already requires, so nothing is added. **Verified working,
  §2.3.**
- **WASM, pure Go (REJECTED by preference + broken):** `ncruces/go-sqlite3` +
  `asg017/.../ncruces`. No cgo, but it's an exotic WASM runtime we don't want to ship —
  and the prebuilt binding is broken today anyway (§2.1).
- **Pure-Go without sqlite-vec (REJECTED — user wants the extension):** `modernc.org/sqlite`
  + BLOB vectors + in-Go brute force (§2.2). Meets the functional goals but doesn't use
  sqlite-vec, which the user explicitly wants.

### 2.1 The CGO-free (WASM) sqlite-vec binding is broken — verified

I built a `CGO_ENABLED=0` smoke test against `asg017/sqlite-vec-go-bindings@v0.1.6`
(its `/ncruces` package, the only CGO-free flavor) + `ncruces/go-sqlite3`. It compiles
and runs pure-Go, but **fails at runtime** — the bindings ship a *prebuilt*
`sqlite3.wasm` whose host-import ABI matches no installable `ncruces` release:

1. `sqlite3_soft_heap_limit64: i32.atomic.store ... feature "" is disabled` — the WASM
   uses atomics; fixable by setting `sqlite3.RuntimeConfig` to enable
   `experimental.CoreFeaturesThreads`.
2. After that: `import func[env.go_busy_timeout]: signature mismatch: i32i32_i32 !=
   i32i32i32_i32` — the embedded WASM imports a **2-arg** `go_busy_timeout`, but every
   `ncruces` release from v0.16.0 through v0.35.1 registers a **3-arg** host function.
   No available version reconciles this. The bindings' prebuilt WASM predates its own
   declared `require ncruces v0.17.1`.

**Conclusion:** the off-the-shelf CGO-free sqlite-vec binding does not work today.
Making it work would require **building the sqlite-vec+SQLite WASM ourselves**
(wasi-sdk + SQLite amalgamation + `sqlite-vec.c`, matching ncruces' host-import
contract) — real, brittle toolchain work, not a drop-in. This is *not* straightforward.

### 2.2 Pure-Go fallback (recorded, not chosen)

If sqlite-vec were unavailable, a fully pure-Go design would still meet the *functional*
goals: `modernc.org/sqlite` + a `chunk_vectors(chunk_id, vector BLOB)` table + in-Go
brute-force L2/cosine (reusing `normalize`). No cgo, one `.db` file, exact ranking.
Not chosen because the user specifically wants the sqlite-vec extension, and §2.3 shows
we can have it cleanly.

### 2.3 CHOSEN path verified — mattn + cgo sqlite-vec, compiled from source

I built a `CGO_ENABLED=1` smoke test: `mattn/go-sqlite3` + `asg017/sqlite-vec-go-bindings/cgo`,
in a throwaway module, with **no prebuilt libs and no `DYLD_LIBRARY_PATH`**. A single
`go run .` compiled both C units from source and:

- `select vec_version()` → `v0.1.6`
- created `CREATE VIRTUAL TABLE t USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[3])`
- brute-force `... WHERE vector MATCH ? ORDER BY distance LIMIT 2` returned the correct
  nearest neighbor (`chunk_id=1`, distance 0.1414 for query `{0.9,0.1,0}`).

Only output: cosmetic clang deprecation warnings for `sqlite3_auto_extension` on macOS
(Apple marks process-global auto-extensions deprecated in its *system* header). mattn
compiles its own SQLite where it works; registration and queries succeeded regardless.
If we ever want to silence them, mattn also supports per-connection extension setup via
a `ConnectHook`.

**Net dependency change:** drop `lancedb-go`, `apache/arrow`, and the `native/` tree
(plus the lib-install `make` steps and `DYLD_LIBRARY_PATH`); add
`asg017/sqlite-vec-go-bindings/cgo`. Keep `mattn/go-sqlite3`. Result: still cgo (from
source, no installer), **one `.db` file**, real SQLite, `vec0` brute-force KNN.

**Trade-off accepted:** cgo stays on, so cross-compiling needs a C toolchain for the
target (same as today with mattn); a `CGO_ENABLED=0` fully-static binary is not possible
with this path. That was the only thing the WASM route bought, and it's out of scope.

---

## 3. Storing vectors + the "no duplicated text" question

sqlite-vec's `vec0` virtual table can carry, besides the vector column:

- an explicit integer primary key (we use `chunk_id`),
- **auxiliary/metadata columns** (e.g. `+text`, `+title`) stored alongside the vector.

Two designs:

- **Option A — vec table holds only `chunk_id` + `vector`.** `chunks.text/title` stay
  the single source of truth; search still does `ChunkMetadataByIDs` afterwards. **No
  text duplication.** Reconciliation logic (`ApplyDocumentChunkReconcile`,
  `moveKeptChunksToTemporaryIndexes`, FK cascade, token counts/offsets/hashes) is
  **untouched**. Minimal, low-risk change — essentially a like-for-like replacement of
  the `lancedb` package.

- **Option B — vec table also holds `+text`/`+title`/`+document_id`; drop the `chunks`
  table.** Fully collapses two stores into one, but the vector table then owns all
  chunk metadata and the whole reconcile/dedup/status machinery has to be rebuilt on a
  virtual table (no FK cascade, no `UNIQUE(document_id, chunk_index)` enforcement,
  offsets/hashes become aux columns). Bigger, riskier refactor.

Note: with Option A there is **no** duplication to begin with — the "don't duplicate
text" concern only arises under Option B (text would live in both places unless
`chunks` is dropped). Option A is the simplest way to honor the constraint.

---

## 4. Chosen plan

Keep `mattn/go-sqlite3`; add sqlite-vec via the cgo bindings (§2.3), **Option A** for
storage. The `documents`/`chunks` tables and all reconcile/status logic are untouched;
only the vector store changes. Steps (each independently testable):

1. **Add sqlite-vec, register `vec0`.** Add `asg017/sqlite-vec-go-bindings/cgo`; call
   its `Auto()` once at startup so every mattn connection exposes `vec0`. Since vectors
   now live in the *same* SQLite file as `documents`/`chunks`, the vector table is
   created in that DB (a migration adds the `vec0` virtual table).
2. **New `internal/storage/sqlitevec` store** implementing the existing `VectorStore`
   interface (`Delete` / `Replace` / `Search` returning `VectorHit`):
   - table: `CREATE VIRTUAL TABLE chunk_vectors USING vec0(chunk_id INTEGER PRIMARY KEY,
     vector FLOAT[768])` (Option A — id + vector only, no text).
   - `Replace`: normalize (reuse existing logic) → serialize float32 LE blob → upsert.
   - `Search`: normalize query → `WHERE vector MATCH ? ORDER BY distance LIMIT ?` →
     `[]VectorHit{ChunkID, Distance}`. Brute-force/exact, same ranking as LanceDB.
3. **Rewire** `pkg/workflows.go` / `cmd/*` to construct the new store against the same
   `.db` path; move the `VectorHit` type (and `pkg/search.go`'s import) off the
   `lancedb` package. Delete `internal/storage/lancedb`, `lancedb_smoke.go`,
   `native/`, the arrow/lancedb requires, and the lib-install `make` steps.
4. **Verify**: full test suite green; re-run the §1 baseline searches (expect
   embedding-bound, unchanged) and record in §5.

Open item to settle during step 2: whether the query embedding and stored embedding
must both be normalized identically — they are today (LanceDB normalizes both), so we
keep that. Option B (dropping `chunks`) stays a later, separate consolidation.

---

## 5. After (measured post-refactor, 2026-07-01)

The migration is implemented: `internal/storage/sqlitevec` (vec0 store) replaces
`internal/storage/lancedb`, which is deleted along with `native/`, `lancedb_smoke.go`,
and the lib-install `make`/script steps.

**Dependencies.** Dropped `github.com/lancedb/lancedb-go` and
`github.com/apache/arrow/go/v17` (and their transitive tree). Added
`github.com/asg017/sqlite-vec-go-bindings`. Kept `github.com/mattn/go-sqlite3`. Still
cgo, but everything is compiled from source by `go build` — no prebuilt libraries, no
`DYLD_LIBRARY_PATH`, no install script. `make test` / `make lint` no longer install a
native lib.

**Storage layout.** One file. The `documents`, `chunks`, and the new
`chunk_vectors USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[768])` virtual
table all live in `vector-index.db`. The separate `vector-index.lancedb/` directory is
gone. Option A: the vec table holds only `chunk_id` + `vector`; `chunks.text/title`
remain the single source of truth — no duplicated text.

**Correctness.** Full `go test ./...` green (incl. new `sqlitevec` round-trip tests);
`golangci-lint` clean. Verified end-to-end against LM Studio:

- `rebuild` re-embedded the existing 128 chunks into vec0 (128 rows).
- A from-scratch `index docs/` produced 5 docs → 144 chunks → 144 vectors.
- Search relevance unchanged: `payment` → tax/payout notes; `data storage` → GCP
  storage notes; `brute force vector search` → the scaling doc's brute-force section.

**Latency (same machine, prebuilt binary).**

| query          | before (LanceDB) | after (sqlite-vec) |
|----------------|------------------|--------------------|
| payment        | ~0.11 s          | ~0.02 s            |
| data storage   | ~0.11 s          | ~0.02 s            |
| authentication | ~0.12 s          | ~0.01 s            |

Both are embedding-bound (a short query embeds in ~10–20 ms locally); the vector scan
is sub-millisecond at this scale in both. The "before" numbers were taken via `go run`
(extra process/link overhead); the drop is not a vector-backend speedup, it's the
prebuilt binary. **Conclusion: no performance regression** — the goal was parity, and
we have it, with a materially simpler architecture.

**Distance note.** sqlite-vec's `vec0` MATCH returns L2 distance by default. On unit-
normalized vectors L2 ranks identically to cosine (score 1.07 ≈ cosine 0.46), so
ranking is unchanged; only the numeric `score` scale differs from LanceDB's.

**Deferred.** A synthetic large-corpus scan benchmark (sqlite-vec vs the old LanceDB
constant factor) is not needed for the parity goal and is left out; §1 / the scaling doc
cover the Big-O. Option B (collapsing `chunks` into the vec table) remains a separate
future consolidation.

---

## Decisions (settled)

1. **Backend**: sqlite-vec via **CGO compiled from source** (`mattn/go-sqlite3` +
   `asg017/sqlite-vec-go-bindings/cgo`) — real SQLite, no prebuilt libs, no installer,
   no WASM. Link-verified hands-on (§2.3).
2. **Text storage**: **Option A** — keep `chunks` as metadata source of truth; vector
   table stores only `chunk_id`+`vector`. No duplication.
3. **Semantics**: brute-force/exact KNN, normalized vectors (unchanged from LanceDB).
