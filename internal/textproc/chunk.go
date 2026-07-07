package textproc

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	minBodyTokens = 32
	// partSeparator joins a section's parts (paragraphs) back together within a chunk.
	partSeparator = "\n\n"
)

// Chunk is a retrieval unit: a titled slice of a document's text with its position and a
// content hash for change detection. It is the output of chunking and the row the storage
// layer persists (ID and DocumentID are assigned during persistence).
type Chunk struct {
	ID          int64
	DocumentID  int64
	ChunkIndex  int
	Title       string
	Text        string
	TokenCount  int
	StartOffset int
	EndOffset   int
	ContentHash string
}

// ChunkPart is a titled piece of text on its way to becoming a Chunk.
type ChunkPart struct {
	Title string
	Text  string
}

// SectionChunkConfig parameterizes how sections become chunks. The format supplies its own
// budget, overlap, fallback title, part splitter, and oversized-part splitter; joining parts
// into budgeted chunks is shared (JoinPartsIntoChunks).
type SectionChunkConfig struct {
	MaxTokens          int
	OverlapTokens      int
	AverageTokenLength int
	FallbackTitle      string
	SplitIntoParts     func(body string) []string
	SplitOversized     OversizedSplitter
}

// ChunkSections packs every section into chunks and assigns them sequential indices and
// offsets.
func ChunkSections(sections []Section, config SectionChunkConfig) []Chunk {
	var parts []ChunkPart
	for _, section := range sections {
		parts = append(parts, sectionParts(section, config)...)
	}

	return BuildChunks(parts, config.AverageTokenLength)
}

// sectionParts splits one section's body into titled parts within the token budget, leaving
// room for the title's own tokens.
func sectionParts(section Section, config SectionChunkConfig) []ChunkPart {
	title := sectionTitle(section.Path, config.FallbackTitle)

	bodyBudget := config.MaxTokens - EstimateTokenCount(title, config.AverageTokenLength)
	if bodyBudget < minBodyTokens {
		bodyBudget = minBodyTokens
	}

	bodies := JoinPartsIntoChunks(
		config.SplitIntoParts(section.Body),
		partSeparator,
		bodyBudget,
		config.AverageTokenLength,
		config.OverlapTokens,
		config.SplitOversized,
	)

	parts := make([]ChunkPart, len(bodies))
	for i, body := range bodies {
		parts[i] = ChunkPart{Title: title, Text: body}
	}

	return parts
}

// BuildChunks turns titled parts into chunks with sequential indices, rune offsets, and a
// content hash of title+text for change detection.
func BuildChunks(parts []ChunkPart, averageTokenLength int) []Chunk {
	chunks := make([]Chunk, 0, len(parts))
	offset := 0
	for _, part := range parts {
		runeCount := utf8.RuneCountInString(part.Text)
		chunks = append(chunks, Chunk{
			ChunkIndex:  len(chunks),
			Title:       part.Title,
			Text:        part.Text,
			TokenCount:  EstimateTokenCount(part.Text, averageTokenLength),
			StartOffset: offset,
			EndOffset:   offset + runeCount,
			ContentHash: HashText(part.Title + "\n" + part.Text),
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
