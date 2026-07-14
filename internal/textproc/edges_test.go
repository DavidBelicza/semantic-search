package textproc

import "testing"

func TestEstimateTokenCountEdgeCases(t *testing.T) {
	if got := EstimateTokenCount("", 4); got != 0 {
		t.Fatalf("empty text should be 0 tokens, got %d", got)
	}
	if got := EstimateTokenCount("abcd", 0); got != 1 {
		t.Fatalf("non-positive average should fall back to the default, got %d", got)
	}
	if got := EstimateTokenCount("abcde", 4); got != 2 {
		t.Fatalf("expected a ceiling division to 2, got %d", got)
	}
}

func TestHardWindowSplitterUsesRuneBudget(t *testing.T) {
	got := HardWindowSplitter(4)("aaaaaaaa", 1) // budget 1 token * 4 = 4 runes

	if len(got) != 2 || got[0] != "aaaa" || got[1] != "aaaa" {
		t.Fatalf("unexpected windows: %#v", got)
	}
}

func TestHardWindowReturnsWholeWhenBudgetNonPositive(t *testing.T) {
	if got := HardWindow("abc", 0); len(got) != 1 || got[0] != "abc" {
		t.Fatalf("expected the whole text, got %#v", got)
	}
}

func TestTailTextReturnsTrimmedWholeWhenShort(t *testing.T) {
	if got := TailText("  hi  ", 100); got != "hi" {
		t.Fatalf("expected the trimmed whole string, got %q", got)
	}
}

func TestLineIndexBreaksPastOffset(t *testing.T) {
	starts := LineStarts([]byte("a\nbb\nccc"))
	if got := LineIndex(starts, 3); got != 1 {
		t.Fatalf("expected line 1 for an offset in the second line, got %d", got)
	}
}
