// Package strategy defines what a strategy is: the complete per-file recipe for turning a
// file's bytes into embedded chunks, plus the types that recipe speaks in. It holds the
// Strategy interface and the pool that selects strategies; the concrete strategies live in
// subpackages (general, markdown, pdf). A strategy does no file I/O and no directory
// walking — the pipeline reads the bytes and hands them in.
package strategy

import (
	"context"
	"io/fs"

	"github.com/davidbelicza/semantic-search/core/storage"
)

// FileRef is the file object the pipeline hands to a strategy: the path plus the already
// obtained file info.
type FileRef struct {
	Path string
	Info fs.FileInfo
}

// Embedder turns texts into vectors by talking to an embedding server. It is the transport
// client: it owns the wire protocol, auth, and retries, but nothing model-specific. It is
// injected into a strategy, because embedding is a per-file operation that belongs to the
// strategy.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingModel carries the model-specific knowledge: the model's id and vector size, and how
// to phrase a document chunk and a query for it. Prompt templates vary per model while the wire
// protocol does not, so this knowledge is kept out of the transport client (see Embedder) and
// injected alongside it. BuildData formats a chunk for indexing; BuildQuery formats a search
// query.
type EmbeddingModel interface {
	Name() string
	Dimensions() int
	BuildData(chunk storage.Chunk) string
	BuildQuery(query string) string
}

// Section is one parsed part of a document: a heading path plus the body text under it. Path
// is the trail of headings that locates the section (e.g. ["Guide", "Payments"]); it is
// empty for content that sits above or outside any heading.
type Section struct {
	Path []string
	Body string
}

// ParsedDocument is the structured result of parsing a file: its sections in reading order.
// It is the hand-off between a strategy's Parse (which produces the structure) and its Chunk
// (which slices it), so the chunker can give each chunk its heading context.
type ParsedDocument struct {
	Sections []Section
}

// Strategy is the whole per-file processing recipe. Every method operates on a single file
// (or its content); nothing here spans files or touches the database.
type Strategy interface {
	// Claims reports whether this strategy handles the given path.
	Claims(path string) bool
	// CreateMetadata builds the document metadata for a claimed file.
	CreateMetadata(file FileRef) (storage.FileMetadata, error)
	// Fingerprint returns a stable hash of the file's content for change detection.
	Fingerprint(content []byte) string
	// Parse decodes the file's raw bytes into a structured document (normalized text
	// organized into sections).
	Parse(content []byte) (ParsedDocument, error)
	// Chunk slices a parsed document into chunks, giving each its heading context.
	Chunk(doc storage.Document, parsed ParsedDocument) ([]storage.Chunk, error)
	// Embed turns chunks into vectors, one per chunk, in order.
	Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error)
}
