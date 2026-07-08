package textproc

import "testing"

func TestTrimBlankLinesKeepsInteriorAndIndentation(t *testing.T) {
	if got := TrimBlankLines("\n\n- item\n\t- nested\n\n"); got != "- item\n\t- nested" {
		t.Fatalf("trim blank lines mismatch: %q", got)
	}
}

func TestNormalizeTextStripsBOMConvertsLineEndingsAndCollapsesBlankLines(t *testing.T) {
	got := NormalizeText([]byte(byteOrderMark + "\r\n\r\n# Title\r\n\r\n\r\n\r\nBody\n\n\n"))
	if got != "# Title\n\nBody" {
		t.Fatalf("normalize mismatch: %q", got)
	}
}
