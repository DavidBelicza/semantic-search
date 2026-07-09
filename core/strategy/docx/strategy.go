// Package docx provides the DOCX strategy. A .docx is a ZIP of XML; this strategy reads
// word/document.xml (and word/styles.xml for heading levels) with the standard library — no
// CGO, no external binary — and maps Word heading paragraphs onto the shared heading-path
// model. It embeds general.GeneralStrategy and overrides only Claims and Parse; metadata,
// fingerprint, chunking, and embedding are inherited.
package docx

import (
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

// docxStrategy embeds GeneralStrategy, inheriting everything but the format-specific claim and
// parse. Chunking is the general paragraph chunker, which already titles sections by heading
// path.
type docxStrategy struct {
	general.GeneralStrategy
}

// NewDocxStrategy builds the DOCX strategy over a GeneralStrategy it embeds for the generic
// steps.
func NewDocxStrategy(base general.GeneralStrategy) strategy.Strategy {
	return docxStrategy{GeneralStrategy: base}
}

func (docxStrategy) Claims(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".docx")
}

// Parse unzips the document and turns its heading-structured body into sections. The bytes are
// self-contained, so no file path is needed here.
func (docxStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	sections, err := extractSections(content)
	if err != nil {
		return strategy.ParsedDocument{}, err
	}

	return strategy.ParsedDocument{Sections: sections}, nil
}
