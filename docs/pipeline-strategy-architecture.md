# Pipeline & Strategy Architecture — specification

Status: **specification.** This document defines the intended code architecture for the
indexing/processing pipeline: the goals, the reasons behind the shape, and the contracts
each part must honor. It is a target to build to, not a description of the current code.

---

## 1. Purpose and goals

The app turns files into searchable vectors through a fixed sequence of steps:
**read → parse → chunk → embed**. Different file formats (Markdown today; txt, json, log,
pdf, docx later) need different implementations of the *format-specific* steps, while the
*orchestration* around them — crawling, change detection, status transitions, chunk
reconciliation, database and vector-store writes — is identical regardless of format.

Goals:

- **Add a new format by adding one small, self-contained unit** — a new strategy — and
  nothing else.
- **Keep orchestration in one place**, written once, unaware of formats.
- **Keep everything hard-coded and explicit** — no runtime plugin machinery, no
  reflection, no config files deciding behaviour.
- **Preserve the cheap-check-gates-expensive-work optimization**: unchanged files are
  never parsed or chunked.

---

## 2. Core principle (the invariant)

> **Strategies own the format-specific "how" of each step. Pipelines own the
> format-agnostic orchestration. The only coupling is a pipeline calling a strategy's
> public interface methods (and an injected embedder).**
>
> Pipelines never contain format-specific logic. Strategies never contain database,
> vector-store, status, reconciliation, or embedding logic.

Everything below follows from this principle.

---

## 3. Strategy

### 3.1 The interface

A single interface defines the format-specific steps plus the extension allow-list.
Embedding is deliberately **not** here (see §4):

```go
type Strategy interface {
    Extensions() []string                                       // allow-list, owned by the strategy
    Read(ctx context.Context, doc storage.Document) (string, error)
    Parse(ctx context.Context, text string) (string, error)
    Chunk(ctx context.Context, in chunker.Input) ([]storage.Chunk, error)
}
```

The strategy is an *interface*; each format is a *type* that implements it.

### 3.2 One implementation per format

Each format is its own type that implements every step and declares its own extensions:

```go
type markdownStrategy struct{ /* preconfigured deps: token limits, overlap, etc. */ }

func (markdownStrategy) Extensions() []string { return []string{".md", ".markdown", ".mdown"} }
func (s markdownStrategy) Read(...)  { ... }
func (s markdownStrategy) Parse(...) { ... }
func (s markdownStrategy) Chunk(...) { ... }
```

Reasons: the format's whole behaviour is readable in one place, and the allow-list lives
with the code that knows what those extensions mean.

### 3.3 Shared behaviour is a free function, not inheritance

When two strategies share a step (e.g. txt and log both read a file as raw UTF-8 text),
that behaviour is a package-level function each strategy calls from its own method:

```go
func ReadFileAsText(ctx context.Context, doc storage.Document) (string, error) { ... }

func (txtStrategy) Read(ctx context.Context, d storage.Document) (string, error) { return ReadFileAsText(ctx, d) }
func (logStrategy) Read(ctx context.Context, d storage.Document) (string, error) { return ReadFileAsText(ctx, d) }
```

No base struct, no embedding-for-reuse. Reuse is by calling.

### 3.4 Strategies are preconfigured and hard-coded

A strategy's settings (token limits, overlap, extension list, normalization rules) are
fixed at construction via constants / struct literals. Strategies are not configured at
runtime.

---

## 4. Embedder — an injected object, not a strategy step

Embedding is identical across formats (same model, same prompts), so it is not part of
the `Strategy` interface. It is a concrete object implementing a small interface,
constructed at the outer layer and injected into the processing pipeline:

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Concrete implementation named for the official model it uses:
// text-embedding-embeddinggemma-300m-qat, 768 dimensions.
type EmbeddingGemma300MQATEmbedder struct{ /* base URL, model id, dims, prompts */ }

func NewEmbeddingGemma300MQATEmbedder(...) *EmbeddingGemma300MQATEmbedder
func (e *EmbeddingGemma300MQATEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error)
```

**Naming rule:** the concrete type is named after the *official* model identifier for
precision — `EmbeddingGemma300MQATEmbedder`, matching `embedder.DefaultModel`
(`text-embedding-embeddinggemma-300m-qat`). It is not named `gemma-4-e2b`, which is a
separate text-generation model.

If a future format ever needs a different embedder, that is a wiring change at the outer
layer, never a strategy change.

---

## 5. StrategyPool

A single pool holds the configured strategies and resolves one for a given path by
consulting each strategy's `Extensions()`:

```go
type Pool struct{ /* []Strategy */ }

func NewPool(strategies ...Strategy) Pool
func (p Pool) Find(path string) (Strategy, bool)   // first strategy whose Extensions() match
func (p Pool) Supports(path string) bool           // any strategy matches
```

The pool is the single source of "which formats are supported" and "which strategy runs
for this file."

---

## 6. Pipelines

Two pipelines, each receiving the same pool; the processing pipeline also receives the
embedder.

### 6.1 Pipeline 1 — "all files, one round" (indexing)

Walks the tree once, keeps only files the pool supports, and upserts their metadata.

Owns: crawling, batching, and **file-level change detection** — hashing file bytes and
comparing to the stored hash to decide whether a file is new/changed/unchanged. This
writes only the **documents** table (status + scan checkpoint) and requires **no parse
or chunk**. Uses the pool solely for the allow-list.

### 6.2 Pipeline 2 — "file by file" (processing)

For each document that needs work: resolve its strategy from the pool, run
**read → parse → chunk** (strategy), then embed the produced chunks with the injected
embedder.

Owns: the status state machine, **chunk-level change detection** (reconciling the chunks
the strategy just produced against existing chunk hashes → new/kept/removed), and all
SQLite / vector-store writes. This writes the **chunks** table and the vector store. It
calls strategy methods for per-format work and the embedder for vectors — nothing else
format-aware.

### 6.3 Change detection is split by which table it touches

- **File-level** (documents table, no parse/chunk) → Pipeline 1.
- **Chunk-level** (chunks table, depends on the strategy's output) → Pipeline 2.

The strategy only *produces* chunks; the pipeline *decides* new/kept/removed.

**Gating invariant:** the cheap file-level check must run before Pipeline 2's per-format
work, so an unchanged file is short-circuited before it is ever parsed or chunked.

---

## 7. Layering: main / cmd / pkg

- **`main`** — the thinnest possible entrypoint. It just runs the app (delegates to the
  bootstrap). No logic.
- **`cmd`** — the CLI surface (cobra commands, flags, argument parsing, output
  formatting). It wires user input to the bootstrap; it holds no processing logic.
- **`pkg`** — the public, replaceable, configurable, **injectable "lego" bootstrap** of
  the logic. This is where the concrete pieces are instantiated and composed:
  - construct the `StrategyPool` via `NewPool(NewMarkdownStrategy(), …)`,
  - construct the `Embedder` (`NewEmbeddingGemma300MQATEmbedder(...)`),
  - construct the stores (SQLite metadata store, sqlite-vec vector store),
  - construct and run the pipelines, injecting the above.

  Everything in `pkg` is expressed as swappable parts behind interfaces, so a consumer
  can replace the embedder, add a strategy, or substitute a store without touching
  internal packages.

Rule of thumb: **`internal/*` provides the parts; `pkg` assembles them; `cmd` exposes
them; `main` starts them.**

---

## 8. Adding a new format (the payoff)

To support, say, `.txt`:

1. Add a `txtStrategy` type implementing `Strategy`, with its own `Extensions()` and its
   own read/parse/chunk (reusing shared functions like `ReadFileAsText` where behaviour
   is identical).
2. Register it in the pool at the outer layer: `NewPool(NewMarkdownStrategy(), NewTxtStrategy())`.

No pipeline changes, no orchestration changes, no embedder changes.

---

## 9. Scope guard (non-goals)

The architecture is justified by the multi-format roadmap, but must stay lean.
Explicitly out of scope:

- No plugin system, registry, or auto-discovery — hard-coded `NewPool(...)` is the whole
  mechanism.
- No abstracting a step that has a single implementation "just in case."
- No ceremony-heavy Pipeline 1 object if a thin function reads clearly — formalize it
  only if it earns its keep.
- No per-strategy embedder unless a format genuinely needs a different model.

Target ceiling: **interface + injected embedder + two pipeline seams + the
main/cmd/pkg layering.** Anything beyond needs a concrete reason.

---

## 10. Implementation sequence

Refactor-only, no behaviour change; each step keeps the test suite green.

1. Introduce the `Strategy` interface; make Markdown a concrete `markdownStrategy`
   implementation; move the extension list into the type; drop any embedder from it.
2. Rename the embedder to `EmbeddingGemma300MQATEmbedder` behind the `Embedder`
   interface; inject it separately.
3. Move pool + embedder construction to `pkg` (the bootstrap); `strategy` exposes
   `NewPool` and strategy constructors only.
4. Create a `pipeline` package; move the status-loop / reconcile / embed orchestration
   there as Pipeline 2 (receiving pool + embedder); leave `strategy` step-only. Keep the
   file-level scan gating Pipeline 2.
5. (Optional) formalize Pipeline 1 as an object for symmetry — only if it earns its keep.
6. Only then add new strategies (txt, json, …), each a new implementation with its own
   allow-list, reusing shared step functions.
