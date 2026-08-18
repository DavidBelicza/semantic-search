// Package epub provides the EPUB strategy. An EPUB is an OCF ZIP container whose package
// document maps resources and whose spine defines their reading order. The strategy follows
// that structure, extracts readable XHTML/SVG content, and maps it onto the shared heading-path
// model. It embeds general.GeneralStrategy and inherits metadata, fingerprinting, chunking, and
// embedding.
package epub

import (
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

type epubStrategy struct {
	general.GeneralStrategy
}

// NewEPUBStrategy builds the EPUB strategy over the GeneralStrategy it embeds.
func NewEPUBStrategy(base general.GeneralStrategy) strategy.Strategy {
	return epubStrategy{GeneralStrategy: base}
}

// Claims reports whether the path is an EPUB publication.
func (epubStrategy) Claims(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".epub")
}

// Parse opens the EPUB container and extracts its text-bearing spine resources in reading
// order. The container is self-contained, so no file path is needed here.
func (epubStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	sections, err := extractSections(content)
	if err != nil {
		return strategy.ParsedDocument{}, err
	}

	return strategy.ParsedDocument{Sections: sections}, nil
}
