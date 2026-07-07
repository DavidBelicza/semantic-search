package strategy

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"

	storage "github.com/davidbelicza/semantic-search/internal/storage/sqlite"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	defaultMarkdownMaxTokens = 350
	defaultOverlapTokens     = 50
	byteOrderMark            = "\uFEFF"
)

var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

// markdownStrategy handles Markdown files. It composes GeneralStrategy for the generic
// per-file steps (metadata, fingerprint, embed) and overrides only the Markdown-specific
// ones: which files it claims, how bytes decode to structured sections (normalization plus
// heading splitting), and how those sections are chunked (structure-aware).
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

func (markdownStrategy) Claims(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown":
		return true
	default:
		return false
	}
}

func (s markdownStrategy) CreateMetadata(file FileRef) (storage.FileMetadata, error) {
	return s.general.CreateMetadata(file)
}

func (s markdownStrategy) Fingerprint(content []byte) string {
	return s.general.Fingerprint(content)
}

func (markdownStrategy) Parse(content []byte) (textproc.ParsedDocument, error) {
	return textproc.ParsedDocument{Sections: splitSections(normalizeMarkdown(content))}, nil
}

func (s markdownStrategy) Chunk(doc storage.Document, parsed textproc.ParsedDocument) ([]storage.Chunk, error) {
	return textproc.ChunkSections(parsed.Sections, textproc.SectionChunkConfig{
		MaxTokens:          s.maxTokens,
		OverlapTokens:      s.overlapTokens,
		AverageTokenLength: s.avgTokenLen(),
		FallbackTitle:      textproc.FileTitleFromPath(doc.AbsolutePath),
		SplitIntoParts:     splitMarkdownParts,
		SplitOversized:     s.splitOversized,
	}), nil
}

func (s markdownStrategy) Embed(ctx context.Context, chunks []storage.Chunk) ([][]float32, error) {
	return s.general.Embed(ctx, chunks)
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

func normalizeMarkdown(content []byte) string {
	text := string(content)
	text = strings.TrimPrefix(text, byteOrderMark)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multipleBlankLines.ReplaceAllString(text, "\n\n")

	return textproc.TrimBlankLines(text)
}

type headingMark struct {
	level int
	line  int
}

func splitSections(source string) []textproc.Section {
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
		sections = append(sections, textproc.Section{Path: pathOf(stack), Body: body})
	}

	return sections
}

func wholeAsSection(source string) []textproc.Section {
	body := strings.TrimSpace(source)
	if body == "" {
		return nil
	}

	return []textproc.Section{{Body: body}}
}

func preambleSection(lines []string, firstHeadingLine int) []textproc.Section {
	if firstHeadingLine <= 0 {
		return nil
	}

	body := strings.TrimSpace(strings.Join(lines[:firstHeadingLine], "\n"))
	if body == "" {
		return nil
	}

	return []textproc.Section{{Body: body}}
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

func headingTextFromLine(line string) string {
	return strings.Trim(strings.TrimLeft(line, "#"), " \t#")
}

// splitMarkdownParts splits a section body into parts: paragraphs separated by blank lines,
// with fenced code kept whole as a single part.
func splitMarkdownParts(body string) []string {
	var parts []string
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
			parts, current = flushMarkdownPart(parts, current)
			continue
		}

		current = append(current, line)
	}

	parts, _ = flushMarkdownPart(parts, current)
	return parts
}

func flushMarkdownPart(parts []string, current []string) ([]string, []string) {
	joined := strings.TrimSpace(strings.Join(current, "\n"))
	if joined == "" {
		return parts, nil
	}

	return append(parts, joined), nil
}

func isFenced(block string) bool {
	return isFenceLine(textproc.FirstLine(block))
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}
