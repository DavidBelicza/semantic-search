// Package config provides the Config strategy, one strategy for every settings format (JSON,
// XML, YAML, INI, .properties) as the code strategy is one strategy for every language. Each
// format has its own parser decoding into one shared tree, so rendering, sectioning, and
// redaction are written once. Sections are titled by key path; the descent is driven by size,
// not nesting depth. Credential values are redacted; see redact.go.
package config

import (
	"strings"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	defaultConfigMaxTokens     = 350
	defaultConfigOverlapTokens = 40
	// Config nests without limit, so past this depth the remainder becomes body text.
	defaultMaxDepth = 4
	// Keeps a data dump (a sitemap with 50,000 URLs, say) from exploding into as many sections.
	defaultMaxSectionChildren = 200
)

type configStrategy struct {
	general.GeneralStrategy
	maxTokens     int
	overlapTokens int
}

// NewConfigStrategy builds the Config strategy over a GeneralStrategy it embeds for the
// generic steps. Values under keys that name a credential are always redacted; see redact.go.
func NewConfigStrategy(base general.GeneralStrategy) strategy.Strategy {
	return configStrategy{
		GeneralStrategy: base,
		maxTokens:       defaultConfigMaxTokens,
		overlapTokens:   defaultConfigOverlapTokens,
	}
}

func (configStrategy) Claims(path string) bool {
	return claimsExtension(path) && !hasExcludedName(path)
}

// Parse only normalizes and carries the source; the format parser is picked in Chunk, which
// has the file path.
func (configStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	source := textproc.NormalizeText(content)
	if isGeneratedContent(source) {
		return strategy.ParsedDocument{}, nil
	}

	return strategy.ParsedDocument{Sections: []strategy.Section{{Body: source}}}, nil
}

func (s configStrategy) Chunk(doc storage.Document, parsed strategy.ParsedDocument) ([]storage.Chunk, error) {
	if len(parsed.Sections) == 0 {
		return nil, nil
	}

	sections := s.sectionsOf(doc.AbsolutePath, parsed.Sections[0].Body)

	return general.ChunkSections(sections, general.SectionChunkConfig{
		MaxTokens:          s.maxTokens,
		OverlapTokens:      s.overlapTokens,
		AverageTokenLength: textproc.DefaultAverageTokenLength,
		FallbackTitle:      general.FileTitleFromPath(doc.AbsolutePath),
		SplitIntoParts:     wholeSection,
		SplitOversized:     splitOversizedConfig,
	}), nil
}

// sectionsOf structures the source. A file that does not parse falls back to its text, so a
// config with a syntax error is still searchable.
func (s configStrategy) sectionsOf(path, source string) []strategy.Section {
	parse := parserFor(path)
	if parse == nil {
		return flatSections(source)
	}

	root, err := parse(source)
	if err != nil {
		return flatSections(source)
	}

	sections := buildSections(root, sectionConfig{
		budgetTokens:       s.maxTokens,
		averageTokenLength: textproc.DefaultAverageTokenLength,
		maxDepth:           defaultMaxDepth,
		maxSectionChildren: defaultMaxSectionChildren,
	})

	if len(sections) == 0 {
		return flatSections(source)
	}

	return sections
}

func flatSections(source string) []strategy.Section {
	body := textproc.TrimBlankLines(source)
	if body == "" {
		return nil
	}

	return []strategy.Section{{Body: body}}
}

func wholeSection(body string) []string {
	if body == "" {
		return nil
	}

	return []string{body}
}

func splitOversizedConfig(part string, budget int) []string {
	average := textproc.DefaultAverageTokenLength

	return textproc.JoinPartsIntoChunks(
		indentedLines(part), "\n", budget, average, 0, textproc.HardWindowSplitter(average),
	)
}

// indentedLines drops blank lines but keeps leading whitespace, so the indentation that carries
// the nesting survives into the chunks of an oversized section.
func indentedLines(text string) []string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}

	return kept
}
