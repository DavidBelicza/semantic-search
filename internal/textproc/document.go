package textproc

// Section is one parsed part of a document: a heading path plus the body text under it.
// Path is the trail of headings that locates the section (e.g. ["Guide", "Payments"]); it
// is empty for content that sits above or outside any heading.
type Section struct {
	Path []string
	Body string
}

// ParsedDocument is the structured result of parsing a file: its sections in reading order.
// It is the hand-off between a strategy's Parse, which produces the structure, and its
// Chunk, which decides how to slice that structure into chunks. Carrying sections (rather
// than a flat string) is what lets the chunker give each chunk its heading context.
type ParsedDocument struct {
	Sections []Section
}
