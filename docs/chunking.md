# Chunking

How a file's text becomes chunks. Parsing produces a document's **sections** (a heading
path + body); chunking slices those sections into token-budget chunks. The shared engine
lives in `strategy/general` (`ChunkSections`); each strategy supplies its own parsing and a
chunk config (budget, overlap, how to split parts).

## The shared engine (`strategy/general`)

For each section:

1. **Title** — the heading path (`Guide > Payments`), or the file name when the section has
   no heading above it.
2. **Pack parts** — group the section's parts (paragraphs) into chunks up to a token
   budget, minus the title's tokens.
3. **Overlap** — prepend a token-sized tail of each chunk onto the next, so context isn't
   lost at boundaries.
4. **Split oversized parts** — a part that exceeds the budget on its own is broken into
   sentences, and hard-windowed as a last resort.

Each chunk records a content hash of `title + "\n" + text`, used for change detection during
reconciliation.

## General (base)

One section from the whole file, split into paragraphs, budget 350 / overlap 50. This is the
structure-agnostic default that other formats inherit unless they override chunking.

## Markdown (`strategy/markdown`)

Splits into sections by Markdown headings (goldmark), keeps code fences whole as a single
part, and splits an oversized fenced block by line instead of by sentence. Budget 350 /
overlap 50.

## PDF (`strategy/pdf`)

Extracts font-annotated text runs (PDFium), then infers structure: headings from font size
relative to the body font, text ordered top-to-bottom in reading order, repeated page
headers/footers stripped, and hyphenated line breaks rejoined — producing sections. It then
inherits the general chunk config (paragraphs, 350 / 50). See
[research/pdf-extraction-engine.md](research/pdf-extraction-engine.md).

## Code (`strategy/code`)

Detects definition boundaries with a Chroma lexer (pure Go, no CGO) and makes one section per
function/class, so each chunk is a coherent unit titled with its nesting path
(`class Invoice > total()`). Definitions are found by token *category* — a `NameFunction` /
`NameClass` introduced by a declaration keyword — never by keyword spelling, so modifiers
(`public`, `static`, `readonly`, `async`) and language differences need no special-casing. A
definition's leading doc-comment and decorators snap onto it. Budget 400 / overlap 40; a
definition over budget is windowed by line (indentation preserved) with overlap.

Two families share the section/heading model and differ only in how a block's extent is found:

- **Brace family** (Go, JS/TS/JSX/TSX, Java, PHP, Rust, C/C++, C#, shell) — nesting by brace
  depth, counting only real punctuation braces, so braces inside strings and comments never
  miscount.
- **Indent family** (Python) — nesting read from leading indentation.

Ruby and SQL are claimed but use a **flat** splitter (whole file, no definition boundaries)
until they get their own splitter; they are still normalized, chunked with overlap, and
embedded. Files whose name marks them minified/bundled, or whose content is minified (a line
over 5000 runes) or carries a generated-code banner, are skipped entirely.

Because the file path is needed to pick the lexer and family, and `Parse` only receives bytes,
the code strategy normalizes in `Parse` and does its sectioning in `Chunk` (which has the
path) — the one place its flow differs from the other strategies.

## Token estimation

Approximate, not a real tokenizer: `ceil(runeCount / averageTokenLength)` with
`averageTokenLength` = 4. Good enough to size chunks against the budget.
