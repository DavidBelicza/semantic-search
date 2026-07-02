package textproc

import (
	"regexp"
	"strings"
)

var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// SplitSentences splits a block of prose into sentences on sentence-ending punctuation.
func SplitSentences(block string) []string {
	marked := sentenceBoundary.ReplaceAllString(block, "$1\x00")
	return NonEmptyTrimmed(strings.Split(marked, "\x00"))
}

// NonEmptyLines splits text into lines, trimming each and dropping empty ones.
func NonEmptyLines(block string) []string {
	return NonEmptyTrimmed(strings.Split(block, "\n"))
}

// NonEmptyTrimmed trims each value and drops the empty ones.
func NonEmptyTrimmed(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}

	return result
}

// FirstLine returns the first line of s (everything before the first newline).
func FirstLine(s string) string {
	if index := strings.IndexByte(s, '\n'); index >= 0 {
		return s[:index]
	}

	return s
}

// LineStarts returns the byte offset of the start of each line in src.
func LineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}

	return starts
}

// LineIndex returns the zero-based index of the line containing the given byte offset.
func LineIndex(starts []int, offset int) int {
	index := 0
	for i, start := range starts {
		if start > offset {
			break
		}
		index = i
	}

	return index
}
