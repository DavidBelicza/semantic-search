package textproc

import "testing"

func TestEstimateTokenCountUsesAverageTokenLength(t *testing.T) {
	if got := EstimateTokenCount("abcdefg", 3); got != 3 {
		t.Fatalf("estimated token count mismatch: want 3, got %d", got)
	}
}
