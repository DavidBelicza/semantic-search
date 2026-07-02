package textproc

import (
	"strings"
	"unicode"
)

// TailText returns the last wantRunes runes of part, trimmed to a word boundary. Used to
// build overlap between consecutive chunks.
func TailText(part string, wantRunes int) string {
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

// JoinOverlap prepends overlap text to a part, separated by a blank line.
func JoinOverlap(overlap string, part string) string {
	if overlap == "" {
		return part
	}

	return overlap + "\n\n" + part
}

// HardWindow splits text into fixed-size rune windows of at most maxRunes runes. It is a
// last-resort splitter for a unit that exceeds the budget on its own.
func HardWindow(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{text}
	}

	runes := []rune(text)
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
