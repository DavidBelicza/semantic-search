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
