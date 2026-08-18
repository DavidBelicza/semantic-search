package html

import (
	"io"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/markup"
)

// extractSections parses web markup and turns its principal heading-structured content into
// sections. The shared markup package also serves publication strategies without coupling them
// to this concrete strategy.
func extractSections(r io.Reader) ([]strategy.Section, error) {
	document, err := markup.Extract(r, markup.WebMode)

	return document.Sections, err
}
