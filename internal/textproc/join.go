package textproc

import "strings"

// JoinPartsIntoChunks fills token-budget chunks from a section's parts: it joins consecutive
// parts with separator until the next part would overflow the budget, then starts a new
// chunk. A part too big for the budget on its own is broken into finer parts by
// splitOversized. Consecutive chunks then overlap by overlapTokens so context is not lost at
// boundaries.
func JoinPartsIntoChunks(parts []string, separator string, budgetTokens, averageTokenLength, overlapTokens int, splitOversized OversizedSplitter) []string {
	var chunks []string
	var current []string
	currentTokens := 0

	for _, part := range parts {
		partTokens := EstimateTokenCount(part, averageTokenLength)
		if partTokens > budgetTokens {
			chunks, current, currentTokens = flushJoined(chunks, current, separator)
			chunks = append(chunks, splitOversized(part, budgetTokens)...)
			continue
		}
		if currentTokens+partTokens > budgetTokens {
			chunks, current, currentTokens = flushJoined(chunks, current, separator)
		}

		current = append(current, part)
		currentTokens += partTokens
	}

	chunks, _, _ = flushJoined(chunks, current, separator)
	return applyOverlap(chunks, overlapTokens, averageTokenLength)
}

// applyOverlap prepends a token-sized tail of each chunk onto the next so context spans the
// boundary between consecutive chunks.
func applyOverlap(chunks []string, overlapTokens, averageTokenLength int) []string {
	if overlapTokens <= 0 || len(chunks) < 2 {
		return chunks
	}

	result := make([]string, len(chunks))
	result[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		overlap := TailText(chunks[i-1], overlapTokens*averageTokenLength)
		result[i] = JoinOverlap(overlap, chunks[i])
	}

	return result
}

func flushJoined(chunks []string, current []string, separator string) ([]string, []string, int) {
	if len(current) == 0 {
		return chunks, nil, 0
	}

	return append(chunks, strings.Join(current, separator)), nil, 0
}
