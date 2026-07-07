package general

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	defaultMaxTokens     = 350
	defaultOverlapTokens = 50
	minBodyTokens        = 32
	// partSeparator joins a section's parts (paragraphs) back together within a chunk.
	partSeparator = "\n\n"
)

var blankLineSeparator = regexp.MustCompile(`\n[ \t]*\n`)

// SectionChunkConfig parameterizes how sections become chunks. A format supplies its own
// budget, overlap, fallback title, part splitter, and oversized-part splitter; joining parts
// into budgeted chunks is shared (textproc.JoinPartsIntoChunks).
type SectionChunkConfig struct {
	MaxTokens          int
	OverlapTokens      int
	AverageTokenLength int
	FallbackTitle      string
	SplitIntoParts     func(body string) []string
	SplitOversized     textproc.OversizedSplitter
}

// chunkPart is a titled piece of text on its way to becoming a storage.Chunk.
type chunkPart struct {
	title string
	text  string
}

// ChunkSections packs every section into chunks and assigns them sequential indices and
// offsets. It is the shared structured-chunking engine the concrete strategies reuse.
func ChunkSections(sections []strategy.Section, config SectionChunkConfig) []storage.Chunk {
	var parts []chunkPart
	for _, section := range sections {
		parts = append(parts, sectionParts(section, config)...)
	}

	return buildChunks(parts, config.AverageTokenLength)
}

// sectionParts splits one section's body into titled parts within the token budget, leaving
// room for the title's own tokens.
func sectionParts(section strategy.Section, config SectionChunkConfig) []chunkPart {
	title := sectionTitle(section.Path, config.FallbackTitle)

	bodyBudget := config.MaxTokens - textproc.EstimateTokenCount(title, config.AverageTokenLength)
	if bodyBudget < minBodyTokens {
		bodyBudget = minBodyTokens
	}

	bodies := textproc.JoinPartsIntoChunks(
		config.SplitIntoParts(section.Body),
		partSeparator,
		bodyBudget,
		config.AverageTokenLength,
		config.OverlapTokens,
		config.SplitOversized,
	)

	parts := make([]chunkPart, len(bodies))
	for i, body := range bodies {
		parts[i] = chunkPart{title: title, text: body}
	}

	return parts
}

// buildChunks turns titled parts into chunks with sequential indices, rune offsets, and a
// content hash of title+text for change detection.
func buildChunks(parts []chunkPart, averageTokenLength int) []storage.Chunk {
	chunks := make([]storage.Chunk, 0, len(parts))
	offset := 0
	for _, part := range parts {
		runeCount := utf8.RuneCountInString(part.text)
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Title:       part.title,
			Text:        part.text,
			TokenCount:  textproc.EstimateTokenCount(part.text, averageTokenLength),
			StartOffset: offset,
			EndOffset:   offset + runeCount,
			ContentHash: textproc.HashText(part.title + "\n" + part.text),
		})
		offset += runeCount
	}

	return chunks
}

// sectionTitle renders a heading path as a title, falling back to the file-derived title for
// sections with no heading above them.
func sectionTitle(path []string, fallback string) string {
	if len(path) > 0 {
		return strings.Join(path, " > ")
	}

	return fallback
}

// FileTitleFromPath is the file's base name without its extension, used as a chunk title when
// no heading structure applies.
func FileTitleFromPath(path string) string {
	if path == "" {
		return ""
	}

	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// splitParagraphs splits a section body into parts: paragraphs separated by blank lines. It
// is the general (structure-agnostic) part splitter.
func splitParagraphs(body string) []string {
	return textproc.NonEmptyTrimmed(blankLineSeparator.Split(body, -1))
}

// splitOversizedProse breaks a part that exceeds the budget into sentences, hard-cut as the
// floor.
func splitOversizedProse(part string, budget int) []string {
	avg := textproc.DefaultAverageTokenLength
	return textproc.JoinPartsIntoChunks(textproc.SplitSentences(part), " ", budget, avg, 0, textproc.HardWindowSplitter(avg))
}
