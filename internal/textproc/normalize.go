package textproc

import (
	"regexp"
	"strings"
)

const byteOrderMark = "\uFEFF"

var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

// NormalizeText applies format-agnostic text hygiene: it strips a leading byte-order mark,
// converts CRLF and lone CR line endings to LF, collapses runs of three or more blank lines
// into a single blank line, and trims leading and trailing blank lines. It is the shared
// normalization used by every text strategy before parsing.
func NormalizeText(content []byte) string {
	text := string(content)
	text = strings.TrimPrefix(text, byteOrderMark)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multipleBlankLines.ReplaceAllString(text, "\n\n")

	return TrimBlankLines(text)
}

// TrimBlankLines removes leading and trailing blank lines while preserving interior
// content and indentation.
func TrimBlankLines(input string) string {
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
