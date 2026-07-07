package strategy

import (
	"context"
	"path/filepath"
	"strings"

	storage "github.com/davidbelicza/semantic-search/internal/storage/sqlite"
)

// pdfStrategy handles PDF files. It composes GeneralStrategy for every generic per-file
// step (metadata, fingerprint, generic chunking, embed) and overrides only what is
// PDF-specific: which files it claims and how bytes decode to text (extraction via the
// injected PDFTextExtractor).
type pdfStrategy struct {
	general   GeneralStrategy
	extractor PDFTextExtractor
}

// NewPDFStrategy builds the PDF strategy over a GeneralStrategy it reuses for the generic
// steps, with the text extractor injected so the engine can be swapped later.
func NewPDFStrategy(general GeneralStrategy, extractor PDFTextExtractor) Strategy {
	return pdfStrategy{general: general, extractor: extractor}
}

func (pdfStrategy) Claims(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func (s pdfStrategy) CreateMetadata(file FileRef) (storage.FileMetadata, error) {
	return s.general.CreateMetadata(file)
}

func (s pdfStrategy) Fingerprint(content []byte) string {
	return s.general.Fingerprint(content)
}

func (s pdfStrategy) Parse(content []byte) (string, error) {
	text, err := s.extractor.Extract(content)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(text), nil
}

func (s pdfStrategy) Chunk(doc storage.Document, text string) ([]storage.Chunk, error) {
	return s.general.Chunk(doc, text)
}

func (s pdfStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	return s.general.Embed(ctx, chunks)
}
