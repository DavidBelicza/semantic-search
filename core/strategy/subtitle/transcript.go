package subtitle

import (
	"html"
	"regexp"
	"strings"
)

const timingMarker = "-->"

var (
	blockBoundary = regexp.MustCompile(`\n[ \t]*\n`)
	markupOrStyle = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)
)

func buildTranscript(source string) string {
	var spoken []string
	for _, block := range blockBoundary.Split(source, -1) {
		spoken = append(spoken, spokenLines(block)...)
	}

	return strings.Join(spoken, "\n")
}

func spokenLines(block string) []string {
	lines := strings.Split(block, "\n")

	timing := timingLineIndex(lines)
	if timing < 0 {
		return nil
	}

	return cleanLines(lines[timing+1:])
}

func timingLineIndex(lines []string) int {
	for index, line := range lines {
		if strings.Contains(line, timingMarker) {
			return index
		}
	}

	return -1
}

func cleanLines(lines []string) []string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		text := cleanLine(line)
		if text == "" {
			continue
		}

		kept = append(kept, text)
	}

	return kept
}

func cleanLine(line string) string {
	stripped := markupOrStyle.ReplaceAllString(line, "")

	return strings.TrimSpace(html.UnescapeString(stripped))
}
