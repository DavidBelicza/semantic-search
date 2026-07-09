package markdown

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

type headingMark struct {
	level int
	line  int
}

func splitSections(source string) []strategy.Section {
	marks := parseHeadings(source)
	lines := strings.Split(source, "\n")
	if len(marks) == 0 {
		return wholeAsSection(source)
	}

	sections := preambleSection(lines, marks[0].line)
	var stack []textproc.HeadingEntry
	for i, mark := range marks {
		stack = textproc.PushHeading(stack, mark.level, headingTextFromLine(lines[mark.line]))
		body := strings.TrimSpace(strings.Join(lines[mark.line+1:sectionEnd(marks, i, len(lines))], "\n"))
		if body == "" {
			continue
		}
		sections = append(sections, strategy.Section{Path: textproc.PathOf(stack), Body: body})
	}

	return sections
}

func wholeAsSection(source string) []strategy.Section {
	body := strings.TrimSpace(source)
	if body == "" {
		return nil
	}

	return []strategy.Section{{Body: body}}
}

func preambleSection(lines []string, firstHeadingLine int) []strategy.Section {
	if firstHeadingLine <= 0 {
		return nil
	}

	body := strings.TrimSpace(strings.Join(lines[:firstHeadingLine], "\n"))
	if body == "" {
		return nil
	}

	return []strategy.Section{{Body: body}}
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

func isFenced(part string) bool {
	return isFenceLine(textproc.FirstLine(part))
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}
