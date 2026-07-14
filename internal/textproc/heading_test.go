package textproc

import "testing"

func TestPushHeadingTracksThePath(t *testing.T) {
	var stack []HeadingEntry
	stack = PushHeading(stack, 1, "Guide")
	stack = PushHeading(stack, 2, "Payments")

	// A same-level heading pops the previous one at that level.
	stack = PushHeading(stack, 2, "Refunds")
	if got := PathOf(stack); len(got) != 2 || got[0] != "Guide" || got[1] != "Refunds" {
		t.Fatalf("path mismatch: %v", got)
	}

	// A shallower heading pops back to the top level.
	stack = PushHeading(stack, 1, "Appendix")
	if got := PathOf(stack); len(got) != 1 || got[0] != "Appendix" {
		t.Fatalf("expected a reset to the top level, got %v", got)
	}
}

func TestPathOfEmptyStack(t *testing.T) {
	if got := PathOf(nil); len(got) != 0 {
		t.Fatalf("expected an empty path, got %v", got)
	}
}
