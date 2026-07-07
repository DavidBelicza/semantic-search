package textproc

import (
	"regexp"
	"strings"
)

var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

// SplitSentences splits a run of prose into sentences on sentence-ending punctuation.
func SplitSentences(text string) []string {
	marked := sentenceBoundary.ReplaceAllString(text, "$1\x00")
	return NonEmptyTrimmed(strings.Split(marked, "\x00"))
}

// NonEmptyLines splits text into lines, trimming each and dropping empty ones.
func NonEmptyLines(text string) []string {
	return NonEmptyTrimmed(strings.Split(text, "\n"))
}

// OversizedSplitter breaks a single part that is larger than the token budget into finer
// parts. The policy is the caller's: split prose into sentences, code into lines, or — at
// the floor, where no natural split remains — hard-cut with HardWindowSplitter. It is the
// contract JoinPartsIntoChunks uses to handle a part that cannot fit a chunk on its own.
type OversizedSplitter func(part string, budgetTokens int) []string

// HardWindowSplitter is the terminal OversizedSplitter: it brute-cuts an unsplittable part
// into fixed-size windows. Use it as the floor when a part has no finer natural split (e.g.
// a single very long sentence).
func HardWindowSplitter(averageTokenLength int) OversizedSplitter {
	return func(part string, budgetTokens int) []string {
		return HardWindow(part, budgetTokens*averageTokenLength)
	}
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
