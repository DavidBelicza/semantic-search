package strategy

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"

	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/textproc"
)

const (
	defaultMarkdownMaxTokens = 350
	defaultOverlapTokens     = 50
	minBodyTokens            = 32
	byteOrderMark            = "\uFEFF"
)

var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

// markdownStrategy handles Markdown files. It composes GeneralStrategy for the generic
// per-file steps (metadata, fingerprint, embed) and overrides only the Markdown-specific
// ones: which files it claims, how bytes decode to text (normalization), and how text is
// chunked (structure-aware, heading path).
type markdownStrategy struct {
	general            GeneralStrategy
	maxTokens          int
	overlapTokens      int
	averageTokenLength int
}

// NewMarkdownStrategy builds the Markdown strategy over a GeneralStrategy it reuses for
// the generic steps.
func NewMarkdownStrategy(general GeneralStrategy) Strategy {
	return markdownStrategy{
		general:            general,
		maxTokens:          defaultMarkdownMaxTokens,
		overlapTokens:      defaultOverlapTokens,
		averageTokenLength: textproc.DefaultAverageTokenLength,
	}
}

// Claims accepts Markdown files by extension — the strategy's own rule.
func (markdownStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown":
		return true
	default:
		return false
	}
}

// CreateMetadata reuses the generic metadata build.
func (s markdownStrategy) CreateMetadata(file FileRef) (storage.FileMetadata, error) {
	return s.general.CreateMetadata(file)
}

// Fingerprint reuses the generic content hash.
func (s markdownStrategy) Fingerprint(content []byte) string {
	return s.general.Fingerprint(content)
}

// Parse decodes the bytes as Markdown text and normalizes it: strip a UTF-8 BOM,
// normalize line endings, collapse blank-line runs, and trim leading/trailing blanks.
func (markdownStrategy) Parse(content []byte) (string, error) {
	text := string(content)
	text = strings.TrimPrefix(text, byteOrderMark)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multipleBlankLines.ReplaceAllString(text, "\n\n")

	return textproc.TrimBlankLines(text), nil
}

// Chunk splits normalized Markdown into structure-aware chunks.
func (s markdownStrategy) Chunk(doc storage.Document, text string) ([]storage.Chunk, error) {
	noteTitle := noteTitleFromPath(doc.AbsolutePath)

	var parts []chunkPart
	for _, current := range splitSections(text) {
		parts = append(parts, s.sectionChunks(current, noteTitle)...)
	}

	return s.buildChunks(parts), nil
}

// Embed reuses the generic embedder.
func (s markdownStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	return s.general.Embed(ctx, chunks)
}

func (s markdownStrategy) avgTokenLen() int {
	if s.averageTokenLength > 0 {
		return s.averageTokenLength
	}

	return textproc.DefaultAverageTokenLength
}

func (s markdownStrategy) sectionChunks(sec section, noteTitle string) []chunkPart {
	title := sectionTitle(sec.path, noteTitle)

	bodyBudget := s.maxTokens - textproc.EstimateTokenCount(title, s.avgTokenLen())
	if bodyBudget < minBodyTokens {
		bodyBudget = minBodyTokens
	}

	bodies := s.applyOverlap(s.packBlocks(splitBlocks(sec.body), bodyBudget))

	parts := make([]chunkPart, len(bodies))
	for i, body := range bodies {
		parts[i] = chunkPart{title: title, text: body}
	}

	return parts
}

func (s markdownStrategy) packBlocks(blocks []string, budget int) []string {
	var parts []string
	var current []string
	currentTokens := 0

	for _, block := range blocks {
		blockTokens := textproc.EstimateTokenCount(block, s.avgTokenLen())
		if blockTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, "\n\n")
			parts = append(parts, s.splitOversized(block, budget)...)
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

func (s markdownStrategy) splitOversized(block string, budget int) []string {
	if isFenced(block) {
		return s.packUnits(textproc.NonEmptyLines(block), budget, "\n")
	}

	return s.packUnits(textproc.SplitSentences(block), budget, " ")
}

func (s markdownStrategy) packUnits(units []string, budget int, separator string) []string {
	var parts []string
	var current []string
	currentTokens := 0

	for _, unit := range units {
		unitTokens := textproc.EstimateTokenCount(unit, s.avgTokenLen())
		if unitTokens > budget {
			parts, current, currentTokens = flushJoined(parts, current, separator)
			parts = append(parts, textproc.HardWindow(unit, budget*s.avgTokenLen())...)
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

func (s markdownStrategy) applyOverlap(parts []string) []string {
	if s.overlapTokens <= 0 || len(parts) < 2 {
		return parts
	}

	result := make([]string, len(parts))
	result[0] = parts[0]
	for i := 1; i < len(parts); i++ {
		overlap := textproc.TailText(parts[i-1], s.overlapTokens*s.avgTokenLen())
		result[i] = textproc.JoinOverlap(overlap, parts[i])
	}

	return result
}

func (s markdownStrategy) buildChunks(parts []chunkPart) []storage.Chunk {
	chunks := make([]storage.Chunk, 0, len(parts))
	offset := 0
	for _, part := range parts {
		runeCount := utf8.RuneCountInString(part.text)
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Title:       part.title,
			Text:        part.text,
			TokenCount:  textproc.EstimateTokenCount(part.text, s.avgTokenLen()),
			StartOffset: offset,
			EndOffset:   offset + runeCount,
			ContentHash: textproc.HashText(part.title + "\n" + part.text),
		})
		offset += runeCount
	}

	return chunks
}

// --- Markdown-specific stateless helpers (headings, sections, fences) ---

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
	document := goldmark.DefaultParser().Parse(gtext.NewReader(src))
	lineStarts := textproc.LineStarts(src)

	var marks []headingMark
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

		marks = append(marks, headingMark{level: heading.Level, line: textproc.LineIndex(lineStarts, headingOffset(heading))})
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

func isFenced(block string) bool {
	return isFenceLine(textproc.FirstLine(block))
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}
