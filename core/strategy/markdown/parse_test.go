package markdown

import "testing"

// TestPreambleSectionBlankAboveFirstHeading covers preambleSection's blank-body branch: the
// first heading is not on line 0, but everything above it is whitespace, so there is no preamble
// section to emit.
func TestPreambleSectionBlankAboveFirstHeading(t *testing.T) {
	sections := splitSections("\n   \n# Title\n\nBody text.")
	for _, s := range sections {
		if len(s.Path) == 0 {
			t.Fatalf("expected no untitled preamble section, got one: %#v", s)
		}
	}
	if len(sections) != 1 || sections[0].Body != "Body text." {
		t.Fatalf("expected a single titled section, got %#v", sections)
	}
}

// TestSplitMarkdownPartsSkipsEmptyRuns covers flushMarkdownPart's empty-join branch, hit when a
// body opens with blank lines (nothing accumulated yet) and between paragraphs separated by
// multiple blank lines.
func TestSplitMarkdownPartsSkipsEmptyRuns(t *testing.T) {
	parts := splitMarkdownParts("\n\nfirst paragraph\n\n\nsecond paragraph\n\n")
	if len(parts) != 2 || parts[0] != "first paragraph" || parts[1] != "second paragraph" {
		t.Fatalf("expected two trimmed parts, got %#v", parts)
	}
}

// TestMarkdownHeadingsWithoutBodyAreStillIndexed covers a note whose prose sits in its
// headings: the deepest heading becomes the section text rather than being dropped.
func TestMarkdownHeadingsWithoutBodyAreStillIndexed(t *testing.T) {
	sections := splitSections("# Alpha\n\n## Beta\n")

	if len(sections) != 1 {
		t.Fatalf("expected one section, got %#v", sections)
	}
	if got := sections[0]; len(got.Path) != 2 || got.Body != "Beta" {
		t.Fatalf("expected the leaf heading as the body, got %#v", got)
	}
}
