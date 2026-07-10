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

# Embedding models — roadmap

Dedicated `EmbeddingModel` implementations for popular models, alongside `GemmaModel`. Each is
a small type (like `GemmaModel`) plus a `PredefinedModel` constant and a `NewModel` case — no
transport changes: they all speak the OpenAI `/v1/embeddings` protocol through the existing
`OpenAIClient`. The value they add over `GeneralModel` is the model's document/query prompt
templates (`BuildData` / `BuildQuery`), which measurably improve retrieval separation.

Only prefix/instruction-based models are listed here (their document vs. query distinction is
carried in the input text, which the model layer owns). Models that select the task via an API
field (Gemini `taskType`, Cohere `input_type`) need a new client/`Standard`, not just a model,
and are out of scope for this section.

Conventions:

- **Matryoshka truncation is out of scope** — each model uses its fixed native vector size.
- **Encode the vector size in the constant name** (e.g. `Nomic768`) so the dimension is visible
  at the call site; the constant maps to the model's native dimension. (`Gemma300mQAT` keeps its
  parameter-based name.)
- Only models with a GGUF that LM Studio can install are included; all five below are verified
  available on Hugging Face.
- **Verify each with a live margin probe** against LM Studio before marking done — a wrong
  prompt string does not error, it silently degrades ranking.

| Model | Constant | Dim | Document prompt | Query prompt | GGUF source | Status |
|---|---|---|---|---|---|---|
| Nomic Embed Text v1.5 | `Nomic768` | 768 | `search_document: ` | `search_query: ` | `nomic-ai/nomic-embed-text-v1.5-GGUF` | todo |
| Multilingual E5 large | `E5Large1024` | 1024 | `passage: ` | `query: ` | `phate334/multilingual-e5-large-gguf` | todo |
| BGE large en v1.5 | `BGELarge1024` | 1024 | (none) | `Represent this sentence for searching relevant passages: ` | `CompendiumLabs/bge-large-en-v1.5-gguf` | todo |
| Qwen3 Embedding 0.6B | `Qwen3Embedding0_6B1024` | 1024 | (none) | `Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: ` | `Qwen/Qwen3-Embedding-0.6B-GGUF` | todo |
| mxbai embed large v1 | `MxbaiLarge1024` | 1024 | (none) | `Represent this sentence for searching relevant passages: ` | `ChristianAzinn/mxbai-embed-large-v1-gguf` | todo |

## Interface improvement — caller-controlled query task

Let the caller pick the query's task/intent — retrieval (search) vs. semantic similarity,
question answering, classification, clustering, etc. — instead of always assuming retrieval.
This is fully doable today over the current OpenAI client with prefix-based models: it just
swaps the task name in `BuildQuery`. Gemma already documents the whole family in the same shape
(`task: search result | query:`, `task: question answering | query:`, `task: classification |
query:`, …); Qwen3's instruction and E5/Nomic prefixes carry the same intent. (A future
param-based client, e.g. Gemini, would map the same task kind to an API field like `taskType` —
not required for this feature.)

Thread a transport-agnostic `taskType` from `Search` into `BuildQuery` (document embedding stays
as-is), so the same index can be queried with different intents. Only the retrieval prompt is
verified so far; validate each additional task name with a live probe.

`taskType` rules:

- **Free-text string.** Convenience constants are provided for the common tasks, but any string
  may be passed. No validation by default — it is the caller's responsibility to pass a value
  the chosen model understands (and one compatible with how the index was built).
- **Always optional, with a default.** Omitting it embeds the query with the model's default
  (retrieval) prompt, so existing callers are unaffected.
- One value per call; no model needs two task types at once (role — document vs. query — is a
  separate axis, handled by `BuildData` vs. `BuildQuery`).

# Wrap-up review — before merge

Cross-cutting cleanup once the embedder/model work above is settled:

- **Inline documentation** — review all doc comments across the changed packages for accuracy
  against the final code.
- **Code and tests** — review for dead code and for stale variable/identifier names left over
  from the embedder/model refactor and renames (e.g. embedder → client, `Model` →
  `EmbeddingModel`).

# README — update chapter

Add a dedicated section to the README showcasing the new capabilities:

- **What semantic search is** — a small table of example search queries and their returned
  results, to show meaning-based matching (query wording differs from the result wording).
- **Switching models** — an example of moving from Gemma to another model (e.g. Nomic) with
  `NewModel` / `NewGeneralModel`.
- **Setting the task** — an example of passing a `taskType` on a search query.
