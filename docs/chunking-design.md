# Chunking Design

Structure-aware Markdown chunking. **Implemented** in
[internal/chunker/markdown.go](../internal/chunker/markdown.go) (chunking) and
[internal/parser/markdown.go](../internal/parser/markdown.go) (normalization), wired
through [strategy.DefaultPool](../internal/strategy/default.go). Replaced the earlier
hard-cut rune-budget placeholder (still present as `HardLimitChunker` for tests).

Related: spec §9 (normalization), §10 (chunking).

---

## Goal

Better **retrieval quality**, not faster chunking. Chunking speed is irrelevant next
to embedding (~16 ms/chunk vs microseconds to parse) — the payoff is chunks that cut on
meaningful boundaries and carry their context.

## Parameters (as shipped)

- **Chunk size: `DefaultMarkdownMaxTokens = 350`** tokens (the precision-favoring end of
  the common RAG sweet spot ~256–512). Bigger chunks dilute the embedding; smaller
  chunks fragment meaning. The model's 2048-token limit is a ceiling, not a target.
- **Overlap: `DefaultOverlapTokens = 50`**, applied only to budget-forced cuts.
- **Title (heading path or note name)** carried in a separate field, not prefixed into
  the text (see "Title vs text" below).

Tension worth remembering: larger chunks = fewer embeddings = faster indexing, but
lower precision. We keep ~350 and gain indexing speed elsewhere (smaller model), not by
inflating chunks.

## Library choice

- **`github.com/yuin/goldmark`** — pure Go, no cgo, CommonMark-compliant. We use it to
  detect real headings (so `#tag` is *not* treated as a heading, and setext headings
  are recognized); sectioning and block splitting are then line-based off those
  headings.
- **Token counting: approximate** (`EstimateTokenCount`, ~`chars/4`). Pure Go, no
  tokenizer dependency — roughly right for EmbeddingGemma's SentencePiece tokenizer on
  English. If near-exact counts are ever needed without cgo,
  `github.com/sugarme/tokenizer` can load the model's `tokenizer.json` — in reserve.
- **Sentence fallback (oversized prose):** a small regex splitter (`sentenceBoundary`).
- **Normalization:** stdlib (`strings`/`regexp`).
- Explicitly **rejected**: `daulet/tokenizers` (cgo); `langchaingo/textsplitter`
  (pulls a whole LangChain-port framework for one utility).

## Boundary priority (spec §10.2)

```
heading section → paragraph → list block → fenced code block → sentence → token window
```
Cut at the highest-level boundary that keeps chunks within budget.

## Overlap semantics

Overlap preserves meaning that straddles a **cut we were forced to make** — it does not
blur clean semantic boundaries.

1. **Headings bound chunks.** A chunk does not span a heading (unless a whole section
   fits in one chunk). A content-free heading emits no chunk; its text still lives in
   the title of its child sections.
2. **Within a section**, pack blocks until the budget, then cut **with overlap**.
3. **An oversized single block** (paragraph/code > budget) is split — sentences for
   prose, lines for code — with a hard token-window final fallback.
4. **Overlap never crosses a heading / topic boundary** (`applyOverlap` runs per
   section). Context across headings comes from the title, not overlap.

## Title vs text (heading context)

Each chunk carries two fields:

- **`title`** — the heading path (`Cloud Notes > Cloud SQL`), or, when the section has
  no heading, the **note name** (filename without extension). Empty only when both are
  absent.
- **`text`** — the raw chunk body (no heading prefix baked in).

The embedder composes EmbeddingGemma's document template per chunk
(`embedder.DocumentInput`): `title: <title> | text: <body>` (empty title → `none`).
This uses the model's dedicated title slot instead of stuffing the heading into the
body. The `title` is stored in the `chunks.title` column so `rebuild` reproduces the
exact embed input. The chunk **content hash covers title + text**, so renaming a
heading re-embeds the affected chunks.

### Illustration
```
# Cloud Notes
## Cloud SQL   → "Managed relational database service…"
```
→ chunk: `title = "Cloud Notes > Cloud SQL"`, `text = "Managed relational database…"`
→ embedded as `title: Cloud Notes > Cloud SQL | text: Managed relational database…`

## Normalization (parser stage, spec §9)

`MarkdownParser` (was a no-op) now strips the UTF-8 BOM, normalizes `\r\n`/`\r`→`\n`,
collapses runs of blank lines, and trims leading/trailing blank lines while preserving
content indentation. Image/embed references (`![[file.pdf]]`, `![](url)`) are **kept**
— the filenames carry signal. Markdown is not rendered to HTML.

## Resolved decisions

- Overlap size: 50 tokens.
- Heading context lives in the **title slot** (not prefixed into stored text).
- Headingless sections fall back to the **note name** as the title.
- Obsidian syntax (`[[wikilinks]]` / `![[embeds]]` / `#tags`) is kept as plain text.

## Known limitations / follow-ups

- **File rename ≠ re-embed.** The title of a headingless note derives from its
  filename, but a rename isn't a content change, so the content-hash skip won't
  re-chunk it → the stored title/embedding goes stale until the file's content changes.
- **Setext headings** leave their `===`/`---` underline in the section body (ATX `#`
  notes unaffected).
- **Token counting stays approximate**; no model tokenizer.
- **Offsets** (`StartOffset`/`EndOffset`) are output-stream positions, not source byte
  offsets (see improvements M1). Unused today.
