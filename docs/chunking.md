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

## The shared markup extractor (`strategy/markup`)

HTML and EPUB both read HTML-family documents, so the walker lives in one place and neither
strategy depends on the other. A `Mode` selects the policy:

| | Web mode (HTML) | Publication mode (EPUB) |
|---|---|---|
| Root | `<main>`, else a lone `<article>`, else the body | the whole body |
| `aside`, `footer` | dropped as page furniture | kept, they carry notes |
| `svg` | dropped | kept, fixed-layout pages hold text in it |
| `img` | ignored | `alt` text is read |
| `hidden`, `aria-hidden`, page breaks | not special | dropped |
| Document title | not reported | from `<head>`, else a standalone SVG title |

Everything else is shared: the tolerant parser, the block and inline rules, whitespace
collapsing, preformatted handling, and the heading stack.

## The shared sectionizer (`strategy/general`)

Every heading-based format (Markdown, PDF, DOCX, HTML, EPUB) assembles its sections with the same
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

## Config (`strategy/config`)

One strategy for every settings format, the way `strategy/code` is one strategy for every
programming language. `.json`, `.xml`, `.yaml`/`.yml`, `.ini`, and `.properties` each have a
small parser, and all of them decode into one shared tree (`node`: key, value, comment,
children). Rendering and sectioning are written once against that tree, so a YAML
file and an XML file describing the same settings produce nearly the same chunk text and a
query matches either. `.toml` is deferred; `.xhtml` belongs to HTML and cannot collide, since
`filepath.Ext` returns only the segment after the last dot.

Like the code strategy, `Parse` only normalizes and carries the source, because the format
parser is chosen from the file extension and `Parse` has no path; the real structuring happens
in `Chunk`.

**Sectioning is driven by size, not by depth.** Config nests without limit, so mapping nesting
depth onto heading level does not work. Instead a subtree is serialized and measured: if it
fits the budget it becomes one section titled with the key path taken to reach it, however deep
it goes; if not, each child opens a section of its own and the descent continues. Leaves
sitting beside nested blocks are emitted under the parent's path, so no setting is orphaned.

Two backstops bound the walk. A depth cap (4) stops the path becoming the title past that
point — the remainder is rendered as body text, so nothing is lost, it just stops being a
heading. A width cap (200 nested blocks) emits a wide node whole, so a data dump such as a
sitemap with 50,000 entries cannot explode into as many titled sections.

Comments are kept (`# ...` above the key). They are usually the only natural language a config
file contains, which makes them the most useful text in it for a meaning-based search. JSON has
no comment syntax, so it contributes structure alone.

Lock files and generated output are skipped by name and by banner, as in the code strategy. A
file that does not parse is still indexed as flat text rather than failing.

Chunking is the shared engine at 350 / 40, with sections kept whole and oversized ones split on
line boundaries.

## Subtitles (`strategy/subtitle`)

A subtitle file is not a transcript. It is a list of on-screen entries, each one an index
number (SubRip) or optional identifier (WebVTT), a timing line holding `-->`, and the lines
spoken while it is displayed. SubRip (`.srt`) and WebVTT (`.vtt`) share one strategy because
they share that shape.

Parsing splits the source on blank lines and, inside each block, takes everything after the
first timing line. What sits before it is metadata and is dropped, which also removes the
WebVTT preamble for free: the `WEBVTT` header and any `NOTE`, `STYLE`, or `REGION` block holds
no timing line, so it yields nothing and needs no special case.

**Only the spoken lines are kept, and they are kept exactly as the file writes them.** Every
subtitle line stays its own line, in file order, with no empty lines between them. Nothing is
joined, reordered, or removed, so a line repeated on screen stays repeated: a line held across
several entries and a line genuinely said twice are indistinguishable in the file, and
collapsing them would delete real dialogue.

Cleaning is limited to what is not dialogue: markup (`<i>`, `<b>`, the WebVTT `<v Speaker>`
voice tag, inline timestamps), positioning overrides in braces such as `{\an8}`, and HTML
entities, which are decoded after the tags are stripped so an escaped `&lt;i&gt;` cannot become
one.

**Timings are discarded.** They survive neither parsing nor chunking, so a search hit gives the
dialogue but not when it was said. Carrying them into the chunk title was tried and removed:
embedding models do not read `01:38:31` as later than `00:00:07`, so queries such as "the
beginning" or "the second minute" ranked no better than chance, while the timestamps competed
with real dialogue for room in every vector. Position is better answered by ordering chunks on
`ChunkIndex` than by asking the vector search.

**The whole file is one section.** Subtitles carry no headings to section on, so `Parse` emits
a single section and the budget splitter does the real work: a feature-length film becomes one
section and roughly 25 to 35 chunks. With no heading path, every chunk falls back to the
file-derived title, exactly as plain text does.

Chunking is inherited unchanged from the general strategy (350 / 50). The transcript arrives as
a single oversized part, so it is split into sentences and packed to the budget, and overlap
still applies because it is added to the final chunk list rather than per part. Overlap earns
its place here, because chunk boundaries fall mid-conversation and a reply separated from its
setup loses what it was answering.

## EPUB (`strategy/epub`)

An EPUB is a ZIP container, not a single document. `META-INF/container.xml` names the package
document, the package document's manifest lists every resource, and its spine gives the reading
order. The strategy follows that chain rather than guessing at filenames, so chapters arrive in
the order the book declares.

Each spine item resolves through the manifest, following `fallback` chains (with cycle
detection) until a readable media type is found. XHTML, HTML, and SVG content documents are
read; anything else is skipped. The navigation document is skipped by its `nav` property, since
indexing it would duplicate the table of contents as prose. Non-linear spine items are kept:
notes, answer keys, and appendices are still part of the book.

Content documents go through the shared `strategy/markup` extractor in publication mode, so
heading structure maps onto the same heading-path model every other format uses. A chunk from a
novel is titled with its chapter, for example `MOBY-DICK; or, THE WHALE. > CHAPTER 60. The
Line.`, which is what makes a hit locatable. When a content document has no heading of its own,
its `<title>`, else the publication title, else the file name supplies the path.

**Parsing is tolerant, as everywhere else.** A malformed spine entry, an unresolvable href, or a
chapter that fails to parse is skipped and the rest of the book is indexed. Extraction only
fails when nothing at all could be read. Real EPUBs are frequently sloppy, and one bad chapter
should not cost the other forty.

**Reading is bounded**, unlike the other formats. A cap on archive entries, a per-entry size
limit, and a total extraction budget keep a decompression bomb from exhausting memory. Encrypted
content documents are reported rather than indexed as ciphertext, and every stored path is
normalized and checked so an entry cannot escape the container.

Chunking is inherited unchanged from the general strategy (350 / 50).

## Token estimation

Approximate, not a real tokenizer: `ceil(runeCount / averageTokenLength)` with
`averageTokenLength` = 4. Good enough to size chunks against the budget.
