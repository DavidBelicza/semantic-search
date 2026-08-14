package subtitle

import (
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

type subtitleStrategy struct {
	general.GeneralStrategy
}

// NewSubtitleStrategy builds the subtitle strategy over the GeneralStrategy it embeds.
func NewSubtitleStrategy(base general.GeneralStrategy) strategy.Strategy {
	return subtitleStrategy{GeneralStrategy: base}
}

// Claims reports whether the path is a SubRip or WebVTT subtitle file.
func (subtitleStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".srt", ".vtt":
		return true
	default:
		return false
	}
}

// Parse keeps the spoken lines, one per line, as a single transcript section.
func (subtitleStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	transcript := buildTranscript(textproc.NormalizeText(content))
	if transcript == "" {
		return strategy.ParsedDocument{}, nil
	}

	return strategy.ParsedDocument{Sections: []strategy.Section{{Body: transcript}}}, nil
}
