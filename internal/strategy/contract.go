// Package strategy defines a Strategy: the complete life of a single file — deciding
// whether it is claimed, building its metadata, fingerprinting it, and turning its bytes
// into embedded chunks. A strategy does no file I/O and no directory walking; the
// pipeline reads the bytes and hands them in. Generic, format-agnostic behaviour lives in
// GeneralStrategy, which concrete strategies (e.g. MarkdownStrategy) compose and override.
package strategy

import (
	"context"
	"io/fs"

	storage "semantic-search/internal/storage/sqlite"
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
	// Parse decodes the file's raw bytes into text (e.g. UTF-8 for text, extraction for
	// binary formats) and normalizes it.
	Parse(content []byte) (string, error)
	// Chunk splits parsed text into chunks.
	Chunk(doc storage.Document, text string) ([]storage.Chunk, error)
	// Embed turns chunks into vectors, one per chunk, in order.
	Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error)
}
