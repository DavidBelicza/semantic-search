package chunker

import (
	"context"
	"testing"
)

func TestEstimateTokenCountUsesAverageTokenLength(t *testing.T) {
	if got := EstimateTokenCount("abcdefg", 3); got != 3 {
		t.Fatalf("estimated token count mismatch: want 3, got %d", got)
	}
}

func TestHardLimitChunkerCutsByEstimatedTokenLimit(t *testing.T) {
	chunker := HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1}
	chunks, err := chunker.Chunk(context.Background(), Input{Text: "abcdefg"})
	if err != nil {
		t.Fatalf("chunk text: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}

	wantTexts := []string{"abc", "def", "g"}
	for i, want := range wantTexts {
		if chunks[i].Text != want {
			t.Fatalf("chunk %d text mismatch: want %q, got %q", i, want, chunks[i].Text)
		}
		if chunks[i].TokenCount > 3 {
			t.Fatalf("chunk %d exceeds token limit: %d", i, chunks[i].TokenCount)
		}
		if chunks[i].ContentHash == "" {
			t.Fatalf("chunk %d hash was not set", i)
		}
	}
}
