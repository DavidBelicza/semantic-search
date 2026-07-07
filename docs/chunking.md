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

## Token estimation

Approximate, not a real tokenizer: `ceil(runeCount / averageTokenLength)` with
`averageTokenLength` = 4. Good enough to size chunks against the budget.
