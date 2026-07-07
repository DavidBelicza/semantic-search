package strategy

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	pdfMaxTokens     = 350
	pdfOverlapTokens = 50
)

// pdfStrategy handles PDF files. It composes GeneralStrategy for every generic per-file step
// (metadata, fingerprint, embed) and overrides only what is PDF-specific: which files it
// claims, how bytes decode to structured sections (font-based heading inference over the
// extracted runs), and the chunk sizing.
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

func (s pdfStrategy) Parse(content []byte) (textproc.ParsedDocument, error) {
	runs, err := s.extractor.ExtractRuns(content)
	if err != nil {
		return textproc.ParsedDocument{}, err
	}

	return textproc.ParsedDocument{Sections: buildSectionsFromRuns(runs)}, nil
}

func (s pdfStrategy) Chunk(doc storage.Document, parsed textproc.ParsedDocument) ([]storage.Chunk, error) {
	return chunkSections(parsed.Sections, sectionChunkConfig{
		maxTokens:          pdfMaxTokens,
		overlapTokens:      pdfOverlapTokens,
		averageTokenLength: textproc.DefaultAverageTokenLength,
		fallbackTitle:      fileTitleFromPath(doc.AbsolutePath),
		splitIntoParts:     splitParagraphs,
		splitOversized:     pdfSplitOversized,
	}), nil
}

func (s pdfStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	return s.general.Embed(ctx, chunks)
}

// pdfSplitOversized breaks a PDF part that exceeds the budget into sentences, hard-cut as
// the floor.
func pdfSplitOversized(part string, budget int) []string {
	avg := textproc.DefaultAverageTokenLength
	return textproc.JoinPartsIntoChunks(textproc.SplitSentences(part), " ", budget, avg, 0, textproc.HardWindowSplitter(avg))
}
