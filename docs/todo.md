# Format support roadmap

Planned and completed file-format strategies. Each format is a strategy subpackage under
`core/strategy/` that embeds `GeneralStrategy` and overrides only what it needs
(`Claims`, `Parse`, and `Chunk` when the format chunks differently). Adding one touches no
other layer, see [architecture.md](architecture.md).

Priority: **P1** next up, **P2** after, **P3** when the corpus needs it.
Status: **done** / **todo** (partial = base exists, needs wiring).

| Strategy | Extensions | Priority | Status | Go library candidates |
|---|---|---|---|---|
| Markdown | `.md`, `.markdown`, `.mdown` | - | done | `github.com/yuin/goldmark` (in use) |
| PDF | `.pdf` | - | done | `github.com/klippa-app/go-pdfium` (in use) |
| General / plain text | `.txt`, `.text`, `.log`, `.rst`, `.org`, `.adoc` | - | done | stdlib only (base strategy; plain-text `Claims` set, pool wiring, shared `textproc.NormalizeText`) |
| Code | `.go`, `.js`, `.ts`, `.jsx`, `.tsx`, `.py`, `.php`, `.java`, `.rb`, `.rs`, `.c`, `.h`, `.cpp`, `.hpp`, `.cs`, `.sh`, `.sql` | - | done | `github.com/alecthomas/chroma/v2` lexer (pure Go); structure-aware for brace + indent families; Ruby/SQL flat-windowed pending own splitter |
| DOCX | `.docx` | - | done | stdlib `archive/zip` + `encoding/xml` (heading sections via `outlineLvl`; tables linearized) |
| HTML | `.html`, `.htm`, `.xhtml` | - | done | `golang.org/x/net/html` (heading sections from `<h1>`-`<h6>`; `main`/`article`/`body` root; script, style, and navigation dropped) |
| Config | `.json`, `.xml`, `.yaml`, `.yml`, `.ini`, `.properties` | - | done | stdlib `encoding/json`, `encoding/xml`, line parser (`.ini`/`.properties`) + `go.yaml.in/yaml/v3`; one tree, key-path sections |
| CSV / TSV | `.csv`, `.tsv` | P2 | todo | stdlib `encoding/csv` |
| XLSX | `.xlsx` | P3 | todo | `github.com/xuri/excelize/v2` (BSD); or stdlib `archive/zip` + `encoding/xml` |
| EPUB | `.epub` | P3 | todo | stdlib `archive/zip` + `golang.org/x/net/html`; alt `github.com/taylorskalyo/goreader` |
| Subtitles | `.srt`, `.vtt` | - | done | stdlib only (spoken lines kept in file order, one transcript section) |

## Facade configuration roadmap

Values a library user cannot reach through the facade today. Each is hardcoded or set from an
unexported constant, so changing it means forking a strategy or implementing an interface
rather than passing an option. Adding one means a field on `Config`, `IndexOptions`, or a
strategy factory in [semanticsearch.go](../semanticsearch.go), threaded down to where the
constant lives now.

Priority: **P1** next up, **P2** after, **P3** when a caller asks for it.
Status: **done** / **todo** (partial = reachable off the facade, not through it).

| Setting | Where it lives now | Priority | Status | Notes |
|---|---|---|---|---|
| Chunk size and overlap | `defaultMaxTokens` / `defaultOverlapTokens` per strategy (350/50 general, markdown; 400/40 code; 350/40 config) | P1 | todo | Markdown, code, and config already hold them as struct fields, so only the factory argument is missing; `general` reads the constants inline. Changing it invalidates an existing index, because every chunk's `ContentHash` covers title+text, so it needs a documented rebuild warning |
| Path excludes | walk in [internal/pipeline/index.go](../internal/pipeline/index.go) | P1 | todo | `IncludeHidden` and `FollowSymlinks` are the only walk knobs; there is no glob or ignore-file exclusion, so `node_modules`, `vendor`, and build output can only be avoided by pointing at a narrower root |
| Rebuild / force re-index | nothing implements it | P1 | todo | [architecture.md](architecture.md) calls `rebuild` the escape hatch for the size+mtime blind spot, but no such feature exists in the code. Either build it or correct that claim |
| Maximum file size | nothing caps it | P2 | todo | One huge file is read whole, chunked, and embedded at full cost |
| Embed retry policy | `MaxRetries` / `BackoffBase` on `client.OpenAIClient` | P2 | partial | The fields are exported and settable when a caller builds the client directly, but `AiEmbedderConfig` forwards only `Timeout` |
| Search fetch cap | `maxSearchChunks` (4096) in [internal/pipeline/search.go](../internal/pipeline/search.go) | P3 | todo | Deliberately fixed and at sqlite-vec's KNN limit; only worth exposing for the Postgres backend, which has no such ceiling |
| HNSW index parameters | `ensureIndex` in [core/storage/pgvector/store.go](../core/storage/pgvector/store.go) | P3 | todo | The index is created with pgvector defaults; `m` and `ef_construction` are the recall/build-time trade-off |
| Average token length | `textproc.DefaultAverageTokenLength` | P3 | todo | Feeds every token estimate; the right value is tokenizer-dependent, so it matters more once non-Gemma models are common |

## Out of scope (for now)

- **Legacy binary office** (`.doc`, `.xls`, `.ppt`): OLE binary, no good pure-Go reader.
  Convert upstream to the Open XML form, or skip.
- **Images and scanned PDFs**: need OCR (e.g. Tesseract), a real feature with a heavy
  dependency and variable quality, not a format add.
- **Audio / video**: transcription, out of scope for a file indexer.
