// Package markdown provides the Markdown strategy. It embeds general.GeneralStrategy for the
// generic per-file steps and overrides only the Markdown-specific ones: which files it
// claims, how bytes decode to structured sections (normalization plus heading splitting),
// and how those sections are chunked (fence-aware).
package markdown

import (
	"path/filepath"
	"strings"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/strategy"
	"github.com/davidbelicza/semantic-search/internal/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	defaultMarkdownMaxTokens = 350
	defaultOverlapTokens     = 50
)

// markdownStrategy embeds GeneralStrategy, inheriting metadata, fingerprint, and embed, and
// overrides claims, parse, and chunk.
type markdownStrategy struct {
	general.GeneralStrategy
	maxTokens          int
	overlapTokens      int
	averageTokenLength int
}

// NewMarkdownStrategy builds the Markdown strategy over a GeneralStrategy it embeds for the
// generic steps.
func NewMarkdownStrategy(base general.GeneralStrategy) strategy.Strategy {
	return markdownStrategy{
		GeneralStrategy:    base,
		maxTokens:          defaultMarkdownMaxTokens,
		overlapTokens:      defaultOverlapTokens,
		averageTokenLength: textproc.DefaultAverageTokenLength,
	}
}

func (markdownStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown":
		return true
	default:
		return false
	}
}

func (markdownStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	return strategy.ParsedDocument{Sections: splitSections(normalizeMarkdown(content))}, nil
}

func (s markdownStrategy) Chunk(doc storage.Document, parsed strategy.ParsedDocument) ([]storage.Chunk, error) {
	return general.ChunkSections(parsed.Sections, general.SectionChunkConfig{
		MaxTokens:          s.maxTokens,
		OverlapTokens:      s.overlapTokens,
		AverageTokenLength: s.avgTokenLen(),
		FallbackTitle:      general.FileTitleFromPath(doc.AbsolutePath),
		SplitIntoParts:     splitMarkdownParts,
		SplitOversized:     s.splitOversized,
	}), nil
}

func (s markdownStrategy) avgTokenLen() int {
	if s.averageTokenLength > 0 {
		return s.averageTokenLength
	}

	return textproc.DefaultAverageTokenLength
}

// splitOversized breaks a part that exceeds the budget into finer parts: fenced code by
// line, prose by sentence, hard-cut as the floor.
func (s markdownStrategy) splitOversized(part string, budget int) []string {
	if isFenced(part) {
		return textproc.JoinPartsIntoChunks(textproc.NonEmptyLines(part), "\n", budget, s.avgTokenLen(), 0, textproc.HardWindowSplitter(s.avgTokenLen()))
	}

	return textproc.JoinPartsIntoChunks(textproc.SplitSentences(part), " ", budget, s.avgTokenLen(), 0, textproc.HardWindowSplitter(s.avgTokenLen()))
}
