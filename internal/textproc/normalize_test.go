package textproc

import "testing"

func TestTrimBlankLinesKeepsInteriorAndIndentation(t *testing.T) {
	if got := TrimBlankLines("\n\n- item\n\t- nested\n\n"); got != "- item\n\t- nested" {
		t.Fatalf("trim blank lines mismatch: %q", got)
	}
}
