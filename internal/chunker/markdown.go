package chunker

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	storage "semantic-search/internal/storage/sqlite"
)

const (
	DefaultMarkdownMaxTokens = 350
	DefaultOverlapTokens     = 50
	minBodyTokens            = 32
)

var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// MarkdownChunker splits Markdown into structure-aware chunks: it keeps sections
// bounded by headings, packs blocks up to a token budget, overlaps consecutive chunks
// within a section, splits oversized blocks by sentence/line, and prefixes each chunk
// with its heading path for retrieval context.
type MarkdownChunker struct {
	MaxTokens          int
	OverlapTokens      int
	AverageTokenLength int
}

func NewMarkdownChunker(maxTokens int, overlapTokens int) MarkdownChunker {
	if maxTokens <= 0 {
		maxTokens = DefaultMarkdownMaxTokens
	}
	if overlapTokens < 0 {
		overlapTokens = 0
	}

	return MarkdownChunker{
		MaxTokens:          maxTokens,
		OverlapTokens:      overlapTokens,
		AverageTokenLength: DefaultAverageTokenLength,
	}
}

func (c MarkdownChunker) Chunk(ctx context.Context, input Input) ([]storage.Chunk, error) {
	noteTitle := noteTitleFromPath(input.Document.AbsolutePath)

	var parts []chunkPart
	for _, current := range splitSections(input.Text) {
		parts = append(parts, c.sectionChunks(current, noteTitle)...)
	}

	return buildChunks(parts, c.averageTokenLength()), nil
}

type chunkPart struct {
	title string
	text  string
}

type headingMark struct {
	level int
	line  int
}

type headingEntry struct {
	level int
	text  string
}

type section struct {
	path []string
	body string
}

func (c MarkdownChunker) sectionChunks(s section, noteTitle string) []chunkPart {
	title := sectionTitle(s.path, noteTitle)

	bodyBudget := c.MaxTokens - EstimateTokenCount(title, c.averageTokenLength())
	if bodyBudget < minBodyTokens {
		bodyBudget = minBodyTokens
	}

	bodies := c.applyOverlap(c.packBlocks(splitBlocks(s.body), bodyBudget))

	parts := make([]chunkPart, len(bodies))
	for i, body := range bodies {
		parts[i] = chunkPart{title: title, text: body}
	}

	return parts
}

// sectionTitle is the heading path for a section, or the note name when the section
// has no heading (a preamble or a headingless note).
func sectionTitle(path []string, noteTitle string) string {
	if len(path) > 0 {
		return strings.Join(path, " > ")
	}

	return noteTitle
}

func noteTitleFromPath(path string) string {
	if path == "" {
		return ""
	}

	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (c MarkdownChunker) packBlocks(blocks []string, budget int) []string {
	var parts []string
	var current []string
	currentTokens := 0

	for _, block := range blocks {
		blockTokens := EstimateTokenCount(block, c.averageTokenLength())
		if blockTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, "\n\n")
			parts = append(parts, c.splitOversized(block, budget)...)
			continue
		}
		if currentTokens+blockTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, "\n\n")
		}

		current = append(current, block)
		currentTokens += blockTokens
	}

	parts, _, _ = flushJoined(parts, current, "\n\n")
	return parts
}

func (c MarkdownChunker) splitOversized(block string, budget int) []string {
	if isFenced(block) {
		return c.packUnits(nonEmptyLines(block), budget, "\n")
	}

	return c.packUnits(splitSentences(block), budget, " ")
}

func (c MarkdownChunker) packUnits(units []string, budget int, separator string) []string {
	var parts []string
	var current []string
	currentTokens := 0

	for _, unit := range units {
		unitTokens := EstimateTokenCount(unit, c.averageTokenLength())
		if unitTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, separator)
			parts = append(parts, c.hardWindow(unit, budget)...)
			continue
		}
		if currentTokens+unitTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, separator)
		}

		current = append(current, unit)
		currentTokens += unitTokens
	}

	parts, _, _ = flushJoined(parts, current, separator)
	return parts
}

func (c MarkdownChunker) hardWindow(unit string, budget int) []string {
	maxRunes := budget * c.averageTokenLength()
	runes := []rune(unit)

	var parts []string
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}

	return parts
}

func (c MarkdownChunker) applyOverlap(parts []string) []string {
	if c.OverlapTokens <= 0 || len(parts) < 2 {
		return parts
	}

	result := make([]string, len(parts))
	result[0] = parts[0]
	for i := 1; i < len(parts); i++ {
		overlap := tailText(parts[i-1], c.OverlapTokens*c.averageTokenLength())
		result[i] = joinOverlap(overlap, parts[i])
	}

	return result
}

func (c MarkdownChunker) averageTokenLength() int {
	if c.AverageTokenLength > 0 {
		return c.AverageTokenLength
	}

	return DefaultAverageTokenLength
}

func splitSections(source string) []section {
	marks := parseHeadings(source)
	lines := strings.Split(source, "\n")
	if len(marks) == 0 {
		return wholeAsSection(source)
	}

	sections := preambleSection(lines, marks[0].line)
	var stack []headingEntry
	for i, mark := range marks {
		stack = pushHeading(stack, mark.level, headingTextFromLine(lines[mark.line]))
		body := strings.TrimSpace(strings.Join(lines[mark.line+1:sectionEnd(marks, i, len(lines))], "\n"))
		if body == "" {
			continue
		}
		sections = append(sections, section{path: pathOf(stack), body: body})
	}

	return sections
}

func wholeAsSection(source string) []section {
	body := strings.TrimSpace(source)
	if body == "" {
		return nil
	}

	return []section{{body: body}}
}

func preambleSection(lines []string, firstHeadingLine int) []section {
	if firstHeadingLine <= 0 {
		return nil
	}

	body := strings.TrimSpace(strings.Join(lines[:firstHeadingLine], "\n"))
	if body == "" {
		return nil
	}

	return []section{{body: body}}
}

func sectionEnd(marks []headingMark, index int, lineCount int) int {
	if index+1 < len(marks) {
		return marks[index+1].line
	}

	return lineCount
}

func parseHeadings(source string) []headingMark {
	src := []byte(source)
	document := goldmark.DefaultParser().Parse(text.NewReader(src))
	lineStarts := computeLineStarts(src)

	var marks []headingMark
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		marks = append(marks, headingMark{level: heading.Level, line: lineIndex(lineStarts, headingOffset(heading))})
		return ast.WalkSkipChildren, nil
	})

	return marks
}

func headingOffset(heading *ast.Heading) int {
	lines := heading.Lines()
	if lines == nil || lines.Len() == 0 {
		return 0
	}

	return lines.At(0).Start
}

func computeLineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}

	return starts
}

func lineIndex(starts []int, offset int) int {
	index := 0
	for i, start := range starts {
		if start > offset {
			break
		}
		index = i
	}

	return index
}

func pushHeading(stack []headingEntry, level int, text string) []headingEntry {
	for len(stack) > 0 && stack[len(stack)-1].level >= level {
		stack = stack[:len(stack)-1]
	}

	return append(stack, headingEntry{level: level, text: text})
}

func pathOf(stack []headingEntry) []string {
	path := make([]string, 0, len(stack))
	for _, entry := range stack {
		path = append(path, entry.text)
	}

	return path
}

func headingTextFromLine(line string) string {
	return strings.Trim(strings.TrimLeft(line, "#"), " \t#")
}

func splitBlocks(body string) []string {
	var blocks []string
	var current []string
	inFence := false

	for _, line := range strings.Split(body, "\n") {
		if isFenceLine(line) {
			inFence = !inFence
			current = append(current, line)
			continue
		}
		if inFence {
			current = append(current, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			blocks, current = flushBlock(blocks, current)
			continue
		}

		current = append(current, line)
	}

	blocks, _ = flushBlock(blocks, current)
	return blocks
}

func flushBlock(blocks []string, current []string) ([]string, []string) {
	joined := strings.TrimSpace(strings.Join(current, "\n"))
	if joined == "" {
		return blocks, nil
	}

	return append(blocks, joined), nil
}

func flushJoined(parts []string, current []string, separator string) ([]string, []string, int) {
	if len(current) == 0 {
		return parts, nil, 0
	}

	return append(parts, strings.Join(current, separator)), nil, 0
}

func splitSentences(block string) []string {
	marked := sentenceBoundary.ReplaceAllString(block, "$1\x00")
	return nonEmptyTrimmed(strings.Split(marked, "\x00"))
}

func nonEmptyLines(block string) []string {
	return nonEmptyTrimmed(strings.Split(block, "\n"))
}

func nonEmptyTrimmed(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}

	return result
}

func tailText(part string, wantRunes int) string {
	runes := []rune(part)
	if len(runes) <= wantRunes {
		return strings.TrimSpace(part)
	}

	start := len(runes) - wantRunes
	for start < len(runes) && !unicode.IsSpace(runes[start]) {
		start++
	}

	return strings.TrimSpace(string(runes[start:]))
}

func joinOverlap(overlap string, part string) string {
	if overlap == "" {
		return part
	}

	return overlap + "\n\n" + part
}

func isFenced(block string) bool {
	return isFenceLine(firstLine(block))
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func firstLine(block string) string {
	if index := strings.IndexByte(block, '\n'); index >= 0 {
		return block[:index]
	}

	return block
}

func buildChunks(parts []chunkPart, averageTokenLength int) []storage.Chunk {
	chunks := make([]storage.Chunk, 0, len(parts))
	offset := 0
	for _, part := range parts {
		runeCount := utf8.RuneCountInString(part.text)
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Title:       part.title,
			Text:        part.text,
			TokenCount:  EstimateTokenCount(part.text, averageTokenLength),
			StartOffset: offset,
			EndOffset:   offset + runeCount,
			ContentHash: HashText(part.title + "\n" + part.text),
		})
		offset += runeCount
	}

	return chunks
}
