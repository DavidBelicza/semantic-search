package textproc

import "testing"

func TestHardWindowSplitsByRuneBudget(t *testing.T) {
	got := HardWindow("abcdefg", 3)
	want := []string{"abc", "def", "g"}
	if len(got) != len(want) {
		t.Fatalf("window count mismatch: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window %d mismatch: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestJoinOverlapPrependsWithBlankLine(t *testing.T) {
	if got := JoinOverlap("tail", "body"); got != "tail\n\nbody" {
		t.Fatalf("join overlap mismatch: %q", got)
	}
	if got := JoinOverlap("", "body"); got != "body" {
		t.Fatalf("empty overlap should return body unchanged: %q", got)
	}
}

func TestTailTextTrimsToWordBoundary(t *testing.T) {
	if got := TailText("alpha beta gamma", 8); got != "gamma" {
		t.Fatalf("tail text mismatch: %q", got)
	}
}
