// Package textproc holds generic, format-agnostic text processing utilities:
// normalization, splitting, windowing, token estimation, and hashing. Format-specific
// logic (e.g. Markdown heading/section handling) lives with its strategy; only reusable,
// stateless primitives belong here.
package textproc

import "math"

// DefaultAverageTokenLength is the fallback average characters-per-token used to
// estimate token counts when no better value is provided.
const DefaultAverageTokenLength = 4

// EstimateTokenCount approximates the token count of text from its rune length.
func EstimateTokenCount(text string, averageTokenLength int) int {
	if text == "" {
		return 0
	}
	if averageTokenLength <= 0 {
		averageTokenLength = DefaultAverageTokenLength
	}

	return int(math.Ceil(float64(len([]rune(text))) / float64(averageTokenLength)))
}
