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
	var dialogue []string
	previous := ""

	for _, block := range blockBoundary.Split(source, -1) {
		spoken := cueDialogue(block)
		if spoken == "" || spoken == previous {
			continue
		}

		dialogue = append(dialogue, spoken)
		previous = spoken
	}

	return strings.Join(dialogue, " ")
}

func cueDialogue(block string) string {
	lines := strings.Split(block, "\n")

	timing := timingLineIndex(lines)
	if timing < 0 {
		return ""
	}

	return cleanDialogue(lines[timing+1:])
}

func timingLineIndex(lines []string) int {
	for index, line := range lines {
		if strings.Contains(line, timingMarker) {
			return index
		}
	}

	return -1
}

func cleanDialogue(lines []string) string {
	spoken := make([]string, 0, len(lines))
	for _, line := range lines {
		text := cleanLine(line)
		if text == "" {
			continue
		}

		spoken = append(spoken, text)
	}

	return strings.Join(spoken, " ")
}

func cleanLine(line string) string {
	stripped := markupOrStyle.ReplaceAllString(line, "")

	return strings.TrimSpace(html.UnescapeString(stripped))
}
