// Package code provides the Code strategy. It embeds general.GeneralStrategy for the generic
// per-file steps and overrides which files it claims, how bytes decode to structured sections,
// and how those sections are chunked. Boundaries are found with a Chroma lexer (pure Go, no
// CGO): definitions are detected by token category, not keyword spelling, and the verbatim
// source of each definition — with its doc-comment — becomes a titled section.
package code

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/strategy"
	"github.com/davidbelicza/semantic-search/internal/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	defaultCodeMaxTokens     = 400
	defaultCodeOverlapTokens = 40
)

// codeStrategy embeds GeneralStrategy, inheriting metadata, fingerprint, and embed, and
// overrides claims, parse, and chunk.
type codeStrategy struct {
	general.GeneralStrategy
	maxTokens     int
	overlapTokens int
}

// NewCodeStrategy builds the Code strategy over a GeneralStrategy it embeds for the generic
// steps.
func NewCodeStrategy(base general.GeneralStrategy) strategy.Strategy {
	return codeStrategy{
		GeneralStrategy: base,
		maxTokens:       defaultCodeMaxTokens,
		overlapTokens:   defaultCodeOverlapTokens,
	}
}

// Claims handles the whitelisted code extensions, minus files whose name marks them as
// minified or bundled artifacts.
func (codeStrategy) Claims(path string) bool {
	return claimsExtension(path) && !hasMinifiedName(path)
}

// Parse normalizes the bytes and carries the source forward as a single section. The real
// sectioning happens in Chunk, which has the file path needed to pick a lexer and splitter.
// Minified or generated content is dropped here so it never reaches the index.
func (codeStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	source := textproc.NormalizeText(content)
	if isExcludedContent(source) {
		return strategy.ParsedDocument{}, nil
	}

	return strategy.ParsedDocument{Sections: []strategy.Section{{Body: source}}}, nil
}

// Chunk lexes the carried source with the file's language, splits it into definition sections,
// and packs those into token-budget chunks via the shared engine.
func (s codeStrategy) Chunk(doc storage.Document, parsed strategy.ParsedDocument) ([]storage.Chunk, error) {
	if len(parsed.Sections) == 0 {
		return nil, nil
	}

	sections := s.splitSource(doc.AbsolutePath, parsed.Sections[0].Body)

	return general.ChunkSections(sections, general.SectionChunkConfig{
		MaxTokens:          s.maxTokens,
		OverlapTokens:      s.overlapTokens,
		AverageTokenLength: textproc.DefaultAverageTokenLength,
		FallbackTitle:      general.FileTitleFromPath(doc.AbsolutePath),
		SplitIntoParts:     wholeSection,
		SplitOversized:     s.splitOversizedCode,
	}), nil
}

// splitSource picks the language family for the path, lexes the source, and produces sections.
func (codeStrategy) splitSource(path, source string) []strategy.Section {
	splitter := splitterFor(path)
	if splitter == nil {
		return flatSections(source)
	}

	return splitter.Split(source, tokenize(path, source))
}

// tokenize lexes source with the Chroma lexer matched from the file name, returning nil when
// no lexer matches (the flat and brace/indent splitters all tolerate an empty token stream).
func tokenize(path, source string) []chroma.Token {
	lexer := lexers.Match(path)
	if lexer == nil {
		return nil
	}

	tokens, err := chroma.Tokenise(lexer, nil, source)
	if err != nil {
		return nil
	}

	return tokens
}

// wholeSection keeps a definition intact as a single part: code is not re-split on blank lines
// the way prose is. Oversized definitions are handled by splitOversizedCode.
func wholeSection(body string) []string {
	return []string{body}
}

// splitOversizedCode windows a definition larger than the budget by lines, preserving
// indentation, with overlap so context is not lost across the cut.
func (s codeStrategy) splitOversizedCode(part string, budget int) []string {
	avg := textproc.DefaultAverageTokenLength

	return textproc.JoinPartsIntoChunks(strings.Split(part, "\n"), "\n", budget, avg, s.overlapTokens, textproc.HardWindowSplitter(avg))
}
