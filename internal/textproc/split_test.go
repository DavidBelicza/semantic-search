package textproc

import (
	"strings"
	"testing"
)

func TestSplitSentencesSplitsOnPunctuation(t *testing.T) {
	got := SplitSentences("First sentence. Second one! Third?")
	want := []string{"First sentence.", "Second one!", "Third?"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("split sentences mismatch: %#v", got)
	}
}

func TestNonEmptyLinesDropsBlankLines(t *testing.T) {
	got := NonEmptyLines("a\n\n  \nb\n")
	if strings.Join(got, "|") != "a|b" {
		t.Fatalf("non-empty lines mismatch: %#v", got)
	}
}

func TestFirstLineReturnsUpToNewline(t *testing.T) {
	if got := FirstLine("head\ntail"); got != "head" {
		t.Fatalf("first line mismatch: %q", got)
	}
	if got := FirstLine("single"); got != "single" {
		t.Fatalf("first line without newline mismatch: %q", got)
	}
}

func TestLineStartsAndLineIndex(t *testing.T) {
	src := []byte("a\nbb\nccc")
	starts := LineStarts(src)
	if len(starts) != 3 {
		t.Fatalf("line starts mismatch: %#v", starts)
	}
	if got := LineIndex(starts, 5); got != 2 {
		t.Fatalf("line index mismatch: want 2, got %d", got)
	}
}
