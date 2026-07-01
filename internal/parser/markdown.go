package parser

import (
	"context"
	"regexp"
	"strings"
)

const byteOrderMark = "\uFEFF"

var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

type MarkdownParser struct{}

func (p MarkdownParser) Parse(ctx context.Context, text string) (string, error) {
	return NormalizeMarkdown(text), nil
}

// NormalizeMarkdown applies conservative cleanup that preserves Markdown structure:
// strip a UTF-8 BOM, normalize line endings to \n, collapse runs of blank lines, and
// trim leading/trailing blank lines. Content indentation is preserved.
func NormalizeMarkdown(input string) string {
	input = strings.TrimPrefix(input, byteOrderMark)
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = multipleBlankLines.ReplaceAllString(input, "\n\n")

	return trimBlankLines(input)
}

func trimBlankLines(input string) string {
	lines := strings.Split(input, "\n")

	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return strings.Join(lines[start:end], "\n")
}
