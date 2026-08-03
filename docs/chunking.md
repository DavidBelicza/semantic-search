# Chunking

How a file's text becomes chunks. Parsing produces a document's **sections** (a heading
path + body); chunking slices those sections into token-budget chunks. The shared engine
lives in `strategy/general` (`ChunkSections`); each strategy supplies its own parsing and a
chunk config (budget, overlap, how to split parts).

Chunks are the retrieval unit: each is embedded and matched independently, but search groups
the matching chunks back into their documents and returns documents (a document's relevance is
its best chunk; `SearchConfig.MaxChunks` caps how many chunks each returned document keeps). A
chunk's title — its heading path — is what appears as the chunk title in results. See
[architecture.md](architecture.md) for the search flow.

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

## The shared sectionizer (`strategy/general`)

Every heading-based format (Markdown, PDF, DOCX, HTML) assembles its sections with the same
`Sectionizer`: it is fed a stream of headings and body paragraphs, and keeps the heading stack
so each section carries its full path.

A heading that never receives body text is emitted as its own section, with the heading text
as the body, rather than being dropped — otherwise a document whose prose sits in its headings
(lab reports, spec lists, link indexes) would index almost nothing. A heading that only
introduces a deeper one is left to its children, so `# A` / `## B` yields one section
(`[A B]`, body `B`) instead of a redundant section per level.

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

Assembling runs into lines is where most of the accuracy lives, because a PDF encodes
positioned glyphs, not lines. Runs are grouped by baseline within a tolerance rather than by an
exact position: a glyph's reported top depends on its shape (an `i` sits higher than an `o`),
so matching exactly would split one line into a fragment per glyph height. A line's runs are
then concatenated directly — PDFium reports the spaces a PDF actually encodes, so inserting a
separator would space out every letter of the documents that emit per-character runs — with a
space added only across a real word gap. A run that repeats the previous one at an overlapping
position is dropped: some PDFs fake bold by drawing the same glyphs twice.

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

## DOCX (`strategy/docx`)

A `.docx` is a ZIP of XML, read with the standard library (`archive/zip` + `encoding/xml`) —
no CGO, no external binary. `word/document.xml` is streamed paragraph by paragraph:
heading paragraphs (identified by `outlineLvl`, resolved from the paragraph or from
`word/styles.xml`, 0-based → 1-based) push onto the shared heading stack; body paragraphs fill
the current section. This produces the same `Section{Path, Body}` structure as Markdown, so
DOCX overrides only `Claims` and `Parse` and inherits the general paragraph chunker (350 / 50)
— chunks are titled with their full heading path (`Guide > Payments`). Tables are linearized
into the surrounding section; headers/footers and footnotes are skipped.

## HTML (`strategy/html`)

Parsed with `golang.org/x/net/html`, a tolerant parser: malformed markup is repaired the way a
browser would, so parsing effectively never fails. Only text nodes are read, so tags never
reach the index.

The tree is flattened into a stream of headings and paragraphs: `<h1>`-`<h6>` push onto the
shared heading stack, block elements separate paragraphs, and inline elements concatenate
without an inserted space (`<b>bo</b><i>ld</i>` stays `bold`). Whitespace runs collapse to a
single space — including the decoded non-breaking space, so it does not survive as a
look-alike character — while `<pre>` is kept verbatim. Entities are decoded by the parser.

Two subtrees never contribute: `script`, `style`, `noscript`, `template`, `svg`, `canvas`, and
`head` are never prose, and `nav`, `footer`, and `aside` are page furniture repeated on every
page. `<header>` is kept, because it often wraps an article's own title.

The indexed subtree is `<main>` when present, else a lone `<article>`, else the body — a page
with several articles is a listing, not a single document, so its first article is not the
root. Like DOCX, HTML overrides only `Claims` and `Parse` and inherits the general paragraph
chunker (350 / 50).

## Token estimation

Approximate, not a real tokenizer: `ceil(runeCount / averageTokenLength)` with
`averageTokenLength` = 4. Good enough to size chunks against the budget.
