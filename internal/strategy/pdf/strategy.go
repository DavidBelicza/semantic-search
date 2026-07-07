// Package pdf provides the PDF strategy. It embeds general.GeneralStrategy and overrides
// only what is PDF-specific: which files it claims and how bytes decode to structured
// sections (font-based heading inference over the extracted runs). Chunking, metadata,
// fingerprinting, and embedding are inherited from GeneralStrategy.
package pdf

import (
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/internal/strategy"
	"github.com/davidbelicza/semantic-search/internal/strategy/general"
)

// pdfStrategy embeds GeneralStrategy and overrides only claims and parse.
type pdfStrategy struct {
	general.GeneralStrategy
	extractor PDFTextExtractor
}

// NewPDFStrategy builds the PDF strategy over a GeneralStrategy it embeds for the generic
// steps, with the text extractor injected so the engine can be swapped later.
func NewPDFStrategy(base general.GeneralStrategy, extractor PDFTextExtractor) strategy.Strategy {
	return pdfStrategy{GeneralStrategy: base, extractor: extractor}
}

func (pdfStrategy) Claims(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func (s pdfStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	runs, err := s.extractor.ExtractRuns(content)
	if err != nil {
		return strategy.ParsedDocument{}, err
	}

	return strategy.ParsedDocument{Sections: buildSectionsFromRuns(runs)}, nil
}
