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

	"semantic-search/internal/ingest"
	"semantic-search/internal/reader"
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

// markdownStrategy handles Markdown files. It reads via the shared generic reader and
// owns its Markdown-specific parse (normalization) and chunk (structure-aware, heading
// path) logic. Its configuration (token limits, overlap) is fixed at construction. The
// chunking steps that use that configuration are methods on this object; only generic,
// markdown-agnostic primitives are pulled from the textproc package.
type markdownStrategy struct {
	maxTokens          int
	overlapTokens      int
	averageTokenLength int
}

func newMarkdownStrategy(maxTokens int, overlapTokens int) markdownStrategy {
	if maxTokens <= 0 {
		maxTokens = defaultMarkdownMaxTokens
	}
	if overlapTokens < 0 {
		overlapTokens = 0
	}

	return markdownStrategy{
		maxTokens:          maxTokens,
		overlapTokens:      overlapTokens,
		averageTokenLength: textproc.DefaultAverageTokenLength,
	}
}

// NewMarkdownStrategy builds the preconfigured Markdown strategy.
func NewMarkdownStrategy() Strategy {
	return newMarkdownStrategy(defaultMarkdownMaxTokens, defaultOverlapTokens)
}

func (markdownStrategy) Extensions() []string {
	return []string{".md", ".markdown", ".mdown"}
}

// Supports reports whether the path's extension is one this strategy handles. It lets the
// strategy act as its own file filter during Ingest.
func (s markdownStrategy) Supports(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	for _, supported := range s.Extensions() {
		if strings.ToLower(supported) == extension {
			return true
		}
	}

	return false
}

// Discovery walks rootPath and returns the files it finds.
func (markdownStrategy) Discovery(rootPath string, options ingest.Options) ([]storage.FileMetadata, error) {
	return ingest.DiscoverFiles(rootPath, options)
}

// Registration records the Markdown files (using this strategy as the file filter) as
// documents.
func (s markdownStrategy) Registration(ctx context.Context, store ingest.MetadataStore, files []storage.FileMetadata) error {
	return ingest.Register(ctx, store, s, files)
}

// Fingerprinting hashes indexed documents to detect content changes.
func (markdownStrategy) Fingerprinting(ctx context.Context, store ingest.Store, failFast bool) error {
	_, err := ingest.FingerprintIndexedDocuments(ctx, store, failFast)
	return err
}

// Read uses the shared generic file reader.
func (markdownStrategy) Read(ctx context.Context, doc storage.Document) (string, error) {
	return reader.ReadFile(ctx, doc)
}

// Parse normalizes Markdown text: strip a UTF-8 BOM, normalize line endings, collapse
// blank-line runs, and trim leading/trailing blank lines. Collapsing blank runs and
// preserving indentation are Markdown-specific editorial choices, so this lives here.
func (markdownStrategy) Parse(ctx context.Context, text string) (string, error) {
	text = strings.TrimPrefix(text, byteOrderMark)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multipleBlankLines.ReplaceAllString(text, "\n\n")

	return textproc.TrimBlankLines(text), nil
}

// Chunk splits normalized Markdown into structure-aware chunks.
func (s markdownStrategy) Chunk(ctx context.Context, doc storage.Document, text string) ([]storage.Chunk, error) {
	noteTitle := noteTitleFromPath(doc.AbsolutePath)

	var parts []chunkPart
	for _, current := range splitSections(text) {
		parts = append(parts, s.sectionChunks(current, noteTitle)...)
	}

	return s.buildChunks(parts), nil
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

// sectionTitle is the heading path for a section, or the note name when the section has
// no heading (a preamble or a headingless note).
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
