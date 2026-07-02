package textproc

import "strings"

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
