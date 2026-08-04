// Package html provides the HTML strategy. Markup is parsed with golang.org/x/net/html — a
// tolerant parser, so malformed documents are repaired rather than rejected — and only text
// nodes are read, so tags never reach the index. It embeds general.GeneralStrategy and
// overrides only Claims and Parse; metadata, fingerprint, chunking, and embedding are
// inherited.
package html

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

// htmlStrategy embeds GeneralStrategy and overrides only claims and parse. Chunking is the
// general paragraph chunker, which already titles sections by heading path.
type htmlStrategy struct {
	general.GeneralStrategy
}

// NewHTMLStrategy builds the HTML strategy over a GeneralStrategy it embeds for the generic
// steps.
func NewHTMLStrategy(base general.GeneralStrategy) strategy.Strategy {
	return htmlStrategy{GeneralStrategy: base}
}

func (htmlStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		return true
	default:
		return false
	}
}

// Parse turns the markup into heading-structured sections. The bytes are self-contained, so
// no file path is needed here.
func (htmlStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	sections, err := extractSections(bytes.NewReader(content))

	return strategy.ParsedDocument{Sections: sections}, err
}
