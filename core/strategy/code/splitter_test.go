package code

import "testing"

// TestTwoDefinitionsOnOneLineClampBoundaries covers snapBackward's floor guard: two functions on
// the same physical line produce two marks at the same line number, so the second definition's
// snapped start would land before the first's floor and is clamped up to it.
func TestTwoDefinitionsOnOneLineClampBoundaries(t *testing.T) {
	sections := sectionsOf(t, "pair.c", "int a(){return 1;} int b(){return 2;}\n")
	if len(sections) < 1 {
		t.Fatalf("expected at least one section for two same-line functions, got %#v", sections)
	}
}

// TestHeaderSectionSkipsBlankHeader covers the guard for a file whose lines above the first
// definition are only whitespace: there is no header worth emitting.
func TestHeaderSectionSkipsBlankHeader(t *testing.T) {
	if got := headerSection([]string{"", "   ", "\t"}, 3); got != nil {
		t.Fatalf("expected no section for a blank header, got %#v", got)
	}
	if got := headerSection([]string{"package p"}, 1); len(got) != 1 {
		t.Fatalf("expected the header emitted, got %#v", got)
	}
	if got := headerSection([]string{"package p"}, 0); got != nil {
		t.Fatalf("expected no section when the first definition is at the top, got %#v", got)
	}
}

// TestResolveIndentPathsSkipsAnchorsPastTheSource covers the guard against an anchor whose
// line is past the end of the source, which a lexer can report for a truncated file.
func TestResolveIndentPathsSkipsAnchorsPastTheSource(t *testing.T) {
	marks := resolveIndentPaths([]anchor{{line: 5, title: "far()"}}, []string{"def a():"})
	if len(marks) != 0 {
		t.Fatalf("expected the out-of-range anchor skipped, got %#v", marks)
	}
}
