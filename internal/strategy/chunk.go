package strategy

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/davidbelicza/semantic-search/internal/storage"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	minBodyTokens = 32
	// partSeparator joins a section's parts (paragraphs) back together within a chunk.
	partSeparator = "\n\n"
)

// sectionChunkConfig parameterizes how sections become chunks. The format supplies its own
// budget, overlap, fallback title, part splitter, and oversized-part splitter; joining parts
// into budgeted chunks is shared (textproc.JoinPartsIntoChunks).
type sectionChunkConfig struct {
	maxTokens          int
	overlapTokens      int
	averageTokenLength int
	fallbackTitle      string
	splitIntoParts     func(body string) []string
	splitOversized     textproc.OversizedSplitter
}

// chunkPart is a titled piece of text on its way to becoming a storage.Chunk.
type chunkPart struct {
	title string
	text  string
}

// chunkSections packs every section into chunks and assigns them sequential indices and
// offsets.
func chunkSections(sections []textproc.Section, config sectionChunkConfig) []storage.Chunk {
	var parts []chunkPart
	for _, section := range sections {
		parts = append(parts, sectionParts(section, config)...)
	}

	return buildChunks(parts, config.averageTokenLength)
}

// sectionParts splits one section's body into titled parts within the token budget, leaving
// room for the title's own tokens.
func sectionParts(section textproc.Section, config sectionChunkConfig) []chunkPart {
	title := sectionTitle(section.Path, config.fallbackTitle)

	bodyBudget := config.maxTokens - textproc.EstimateTokenCount(title, config.averageTokenLength)
	if bodyBudget < minBodyTokens {
		bodyBudget = minBodyTokens
	}

	bodies := textproc.JoinPartsIntoChunks(
		config.splitIntoParts(section.Body),
		partSeparator,
		bodyBudget,
		config.averageTokenLength,
		config.overlapTokens,
		config.splitOversized,
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

// fileTitleFromPath is the file's base name without its extension, used as a chunk title when
// no heading structure applies.
func fileTitleFromPath(path string) string {
	if path == "" {
		return ""
	}

	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
