package textproc

import (
	"strings"
	"testing"
)

func TestJoinPartsIntoChunksPacksToBudget(t *testing.T) {
	parts := []string{"aaaa", "bbbb", "cccc"} // 1 token each at average length 4

	chunks := JoinPartsIntoChunks(parts, " ", 2, 4, 0, HardWindowSplitter(4))

	if len(chunks) != 2 || chunks[0] != "aaaa bbbb" || chunks[1] != "cccc" {
		t.Fatalf("unexpected packing: %#v", chunks)
	}
}

func TestJoinPartsIntoChunksSplitsOversizedPart(t *testing.T) {
	parts := []string{"aaaaaaaaaaaa"} // 12 runes = 3 tokens, over the budget of 2

	chunks := JoinPartsIntoChunks(parts, " ", 2, 4, 0, HardWindowSplitter(4))

	// HardWindow cuts it into 8-rune windows.
	if len(chunks) != 2 || chunks[0] != "aaaaaaaa" || chunks[1] != "aaaa" {
		t.Fatalf("unexpected oversized split: %#v", chunks)
	}
}

func TestApplyOverlapPrependsTail(t *testing.T) {
	out := applyOverlap([]string{"aa bb cc dd", "next"}, 1, 5)

	if out[0] != "aa bb cc dd" {
		t.Fatalf("first chunk changed: %q", out[0])
	}
	if !strings.Contains(out[1], "next") || !strings.Contains(out[1], "\n\n") {
		t.Fatalf("overlap not joined onto the next chunk: %q", out[1])
	}
}

func TestApplyOverlapNoopWhenDisabledOrSingle(t *testing.T) {
	if got := applyOverlap([]string{"a", "b"}, 0, 4); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("zero overlap should not change chunks: %#v", got)
	}
	if got := applyOverlap([]string{"only"}, 5, 4); len(got) != 1 || got[0] != "only" {
		t.Fatalf("single chunk should be unchanged: %#v", got)
	}
}
