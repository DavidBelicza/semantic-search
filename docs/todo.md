# Format support — roadmap

Planned and completed file-format strategies. Each format is a strategy subpackage under
`internal/strategy/` that embeds `GeneralStrategy` and overrides only what it needs
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

# Library API — refactor roadmap

Turn the project into a library-first API. The packages a consumer needs to name or extend
move **whole** from `internal/` to a public `core/` tree — no aliases, and no package is split
(a package either stays entirely in `internal/` or moves entirely to `core/`). The interfaces
then live in their natural domain packages (`core/strategy`, `core/storage`, `core/embedder`),
imported directly. A root `semanticsearch` facade provides the convenience layer (`Engine` +
constructors); its strategy constructors are factories, so `NewEngine` injects the embedder
into each strategy under the hood — `Embed` and `Claims` stay on the Strategy interface.
Phase 1 unlocks injection; Phase 2 adds implementations behind the same interfaces.

Package moves (each moved as a whole unit):

- **→ `core/`:** `embedder`, `storage`, `strategy` (with all subpackages)
- **stay in `internal/`:** `fs`, `pipeline`, `textproc` — plumbing users never name; the public
  `core/*` packages may still import them (a public package importing an internal one in the
  same module is legal).

| Step | Change | Phase | Status |
|---|---|---|---|
| Move to core | Move whole packages `internal/{embedder,storage,strategy}` → `core/{embedder,storage,strategy}`; update import paths; `internal/{fs,pipeline,textproc}` stay | 1 | todo |
| Store interfaces | Define `Storage` (metadata/chunks) and `VectorStorage` interfaces in `core/storage` from the current sqlite/sqlitevec methods; pipeline depends on the interfaces, not concrete stores | 1 | todo |
| Embedder injection | Facade strategy constructors are factories; `NewEngine` builds each strategy with the injected embedder (`general.NewGeneralStrategy(embedder)`), keeping `Embed` and `Claims` unchanged | 1 | todo |
| Embedder API | `NewAiEmbedder(AiEmbedderConfig{Standard, BaseURL, Model, Dimensions})` with typed `StandardOpenAI` const | 1 | todo |
| Dup validation | Facade constructors carry each built-in's extensions (custom strategies supply their own); `NewEngine` errors on duplicate extensions | 1 | todo |
| Store constructors | Per-type: `NewSQLiteStorage(path)`, `NewSQLiteVectorStorage(path)` (returning `core/storage` interfaces) | 1 | todo |
| Strategy constructors | Factories: `NewMarkdownStrategy()`, `NewPDFStrategy()`, `NewCodeStrategy()`, `NewDocxStrategy()`, `NewTextStrategy()` | 1 | todo |
| Facade | Root `semanticsearch` package absorbs `pkg/`: `Index`/`Search` become `Engine` methods, `build.go` wiring splits into `NewEngine` + the constructors, and `IndexOptions`/`SearchResult` move here. Consumers import `core/*` directly for interfaces (no aliases) | 1 | todo |
| Remove CLI | After `pkg/` logic has migrated to the facade, delete `pkg/`, `cmd/`, `main.go`; drop cobra / pflag / mousetrap from `go.mod` | 1 | todo |
| E2E tests | Replace the CLI harness: deterministic e2e (fake embedder) + live e2e (real server, env-gated) | 1 | todo |
| Postgres store | `core/storage/postgres` implementing `Storage` (CGO-free path when sqlite isn't imported) | 2 | todo |
| pgvector | `VectorStorage` on pgvector; document split-DB (metadata and vectors in separate databases) | 2 | todo |
| More embedders | Additional `Standard` values behind `NewAiEmbedder` (e.g. Cohere) | 2 | todo |
