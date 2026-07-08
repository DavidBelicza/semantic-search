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
| DOCX | `.docx` | P1 | todo | stdlib `archive/zip` + `encoding/xml`; alt `github.com/nguyenthenguyen/docx`, `github.com/sajari/docconv` (**shells out to external binaries**) |
| Config | `.json`, `.yaml`, `.yml`, `.toml`, `.ini`, `.properties` | P2 | todo | stdlib (v1, text); `gopkg.in/yaml.v3`, `encoding/json` (optional, key-path structure) |
| HTML | `.html`, `.htm` | P2 | todo | `golang.org/x/net/html`; alt `github.com/PuerkitoBio/goquery` |
| CSV / TSV | `.csv`, `.tsv` | P2 | todo | stdlib `encoding/csv` |
| XLSX | `.xlsx` | P3 | todo | `github.com/xuri/excelize/v2` (BSD); or stdlib `archive/zip` + `encoding/xml` |
| EPUB | `.epub` | P3 | todo | stdlib `archive/zip` + `golang.org/x/net/html`; alt `github.com/taylorskalyo/goreader` |
| Subtitles | `.srt`, `.vtt`, `.ass`, `.ssa` | P3 | todo | stdlib (`.srt`/`.vtt`); `github.com/asticode/go-astisub` (`.ssa`/`.ass`/`.ttml` too) |

## Notes

- **General / plain text** and **Code** and **Config** are all UTF-8 text, so they reuse the
  base structured chunker; the main work is choosing which extensions to claim (avoid
  minified bundles, lock files, generated output). Code is a *separate* strategy so it can
  later chunk by structure (functions/blocks) instead of paragraphs.
- **DOCX / XLSX / PPTX / EPUB** are all ZIP + XML — pure-Go extraction with the standard
  library is possible; a third-party library mainly saves fiddly parsing.
- **Tabular formats (CSV, TSV, XLSX)** need a row-as-record representation: detect the header
  row and prefix each row's cells with their column names, or the embeddings carry no
  meaning. Value is high for text-rich sheets, low for number-heavy ones.
- **Heading-aware formats** (HTML `h1–h6`, DOCX heading styles) map cleanly onto the existing
  section/heading-path model and reuse `textproc.PushHeading`/`PathOf`.
- **Purity of dependencies** — every *primary* candidate above is pure Go, so no format add
  requires a native/CGO or external-binary dependency. The only non-pure-Go options are
  optional or alternates: `go-tree-sitter` (CGO, only if we want structure-aware code
  chunking) and `sajari/docconv` (shells out to external CLIs; not needed since stdlib
  handles DOCX). `go-pdfium` is already used in its pure-Go WebAssembly mode. Note this is
  about the *format* layer only — the storage layer already uses CGO (`mattn/go-sqlite3` and
  the sqlite-vec bindings), so the binary is not CGO-free regardless.

## Out of scope (for now)

- **Legacy binary office** (`.doc`, `.xls`, `.ppt`) — OLE binary, no good pure-Go reader.
  Convert upstream to the Open XML form, or skip.
- **Images and scanned PDFs** — need OCR (e.g. Tesseract): a real feature with a heavy
  dependency and variable quality, not a format add.
- **Audio / video** — transcription; out of scope for a file indexer.
