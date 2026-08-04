package general

import "testing"

func TestSectionizerBuildsHeadingPaths(t *testing.T) {
	s := NewSectionizer("\n\n")
	s.AddHeading(1, "Guide")
	s.AddHeading(2, "Payments")
	s.AddBody("pay on time")
	s.AddBody("second paragraph")
	s.AddHeading(2, "Refunds")
	s.AddBody("five days")

	sections := s.Sections()
	if len(sections) != 2 {
		t.Fatalf("expected two sections, got %#v", sections)
	}
	if got := sections[0]; len(got.Path) != 2 || got.Path[1] != "Payments" ||
		got.Body != "pay on time\n\nsecond paragraph" {
		t.Fatalf("first section mismatch: %#v", got)
	}
	if got := sections[1]; got.Path[1] != "Refunds" || got.Body != "five days" {
		t.Fatalf("second section mismatch: %#v", got)
	}
}

func TestSectionizerBodyBeforeAnyHeadingHasEmptyPath(t *testing.T) {
	s := NewSectionizer("\n\n")
	s.AddBody("preamble")
	s.AddHeading(1, "Title")
	s.AddBody("body")

	sections := s.Sections()
	if len(sections) != 2 || len(sections[0].Path) != 0 || sections[0].Body != "preamble" {
		t.Fatalf("expected an untitled preamble section, got %#v", sections)
	}
}

func TestSectionizerIgnoresEmptyInput(t *testing.T) {
	s := NewSectionizer("\n\n")
	s.AddHeading(1, "   ")
	s.AddBody("  \n ")

	if sections := s.Sections(); len(sections) != 0 {
		t.Fatalf("expected no sections for blank input, got %#v", sections)
	}
}

func TestSectionizerEmitsLeafHeadingWithoutBody(t *testing.T) {
	// A document whose prose sits in its headings: the deepest heading becomes the body so the
	// text is still indexed, and the whole chain stays as the path.
	s := NewSectionizer("\n\n")
	s.AddHeading(1, "Alpha")
	s.AddHeading(2, "Beta")

	sections := s.Sections()
	if len(sections) != 1 {
		t.Fatalf("expected one section, got %#v", sections)
	}
	if got := sections[0]; len(got.Path) != 2 || got.Body != "Beta" {
		t.Fatalf("expected the leaf heading as the body, got %#v", got)
	}
}

func TestSectionizerLeavesParentHeadingToItsChildren(t *testing.T) {
	s := NewSectionizer("\n\n")
	s.AddHeading(1, "Parent")
	s.AddHeading(2, "Child")
	s.AddBody("body")

	if sections := s.Sections(); len(sections) != 1 || sections[0].Body != "body" {
		t.Fatalf("expected only the child's section, got %#v", sections)
	}
}

func TestSectionizerEmitsEachSiblingLeaf(t *testing.T) {
	s := NewSectionizer("\n\n")
	s.AddHeading(1, "Alpha")
	s.AddHeading(2, "Beta")
	s.AddHeading(2, "Gamma")

	sections := s.Sections()
	if len(sections) != 2 || sections[0].Body != "Beta" || sections[1].Body != "Gamma" {
		t.Fatalf("expected a section per sibling leaf, got %#v", sections)
	}
}
