// Package strategy defines a Strategy: the complete life of a single file — deciding
// whether it is claimed, building its metadata, fingerprinting it, and turning its bytes
// into embedded chunks. A strategy does no file I/O and no directory walking; the
// pipeline reads the bytes and hands them in. Generic, format-agnostic behaviour lives in
// GeneralStrategy, which concrete strategies (e.g. MarkdownStrategy) compose and override.
package strategy

import (
	"context"
	"io/fs"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

// FileRef is the file object the pipeline hands to a strategy: the path plus the already
// obtained file info. The strategy reads no filesystem state of its own from it beyond
// what is needed to describe the file.
type FileRef struct {
	Path string
	Info fs.FileInfo
}

// Embedder turns texts into vectors. It is injected into a strategy (via GeneralStrategy),
// because embedding is a per-file operation that belongs to the strategy.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// TextRun is one run of text from a PDF with the font size and position needed to infer
// structure (headings versus body). FontSize is the rendered size in points; X and Y are
// the run's left and top position in PDF points (higher Y is higher on the page).
type TextRun struct {
	Text     string
	FontSize float64
	X        float64
	Y        float64
	Page     int
}

// PDFTextExtractor extracts positioned, font-annotated text runs from a PDF's raw bytes. It
// is injected into the PDF strategy so the underlying extraction engine can be swapped
// without touching the strategy. Returning runs (rather than flat text) keeps the
// heading-inference logic engine-independent.
type PDFTextExtractor interface {
	ExtractRuns(content []byte) ([]TextRun, error)
}

// Strategy is the whole per-file processing recipe. Every method operates on a single
// file (or its content); nothing here spans files or touches the database.
type Strategy interface {
	// Claims reports whether this strategy handles the given path. The rule is the
	// strategy's own — an extension, content sniffing, or "everything".
	Claims(path string) bool
	// CreateMetadata builds the document metadata for a claimed file.
	CreateMetadata(file FileRef) (storage.FileMetadata, error)
	// Fingerprint returns a stable hash of the file's content for change detection.
	Fingerprint(content []byte) string
	// Parse decodes the file's raw bytes into a structured document: normalized text
	// organized into sections (e.g. by Markdown heading, or by PDF font structure).
	Parse(content []byte) (textproc.ParsedDocument, error)
	// Chunk slices a parsed document into chunks, giving each its heading context.
	Chunk(doc storage.Document, parsed textproc.ParsedDocument) ([]storage.Chunk, error)
	// Embed turns chunks into vectors, one per chunk, in order.
	Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error)
}
