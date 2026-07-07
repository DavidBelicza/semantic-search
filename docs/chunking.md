# Chunking

How a file's text becomes chunks. Lives in the strategy (`internal/strategy`); the
pipeline just hands over the bytes.

## Markdown (`markdownStrategy.Chunk`)

Structure-aware, so a chunk stays within one section and carries its heading context.

1. **Split into sections** by Markdown headings (parsed with goldmark). Each section's
   title is its heading path (`Guide > Payments`); a headingless note uses the file name.
2. **Pack blocks** (paragraphs, list items, code fences) into chunks up to a token
   budget (`defaultMarkdownMaxTokens` = 350), minus the title's tokens.
3. **Overlap** consecutive chunks within a section by `defaultOverlapTokens` (50) so
   context isn't lost at boundaries.
4. **Split oversized blocks** that exceed the budget on their own — by sentence for
   prose, by line inside code fences — and hard-window as a last resort.

Each chunk records a content hash of `title + "\n" + text`, used for change detection
during reconciliation.

## Generic (`GeneralStrategy.Chunk`)

For non-Markdown files: fixed token-budget windows (`generalMaxTokens` = 300), no
structure awareness. This is the fallback any format inherits unless it overrides `Chunk`.

## Token estimation

Approximate, not a real tokenizer: `ceil(runeCount / averageTokenLength)` with
`averageTokenLength` = 4. Good enough to size chunks against the budget.
