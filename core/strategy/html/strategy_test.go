package html

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func newHTML() strategy.Strategy {
	return NewHTMLStrategy(general.NewGeneralStrategy(nil, nil))
}

// parseSections runs the strategy's parse for markup, returning the structured sections so
// tests can assert boundaries and heading paths.
func parseSections(t *testing.T, markup string) []strategy.Section {
	t.Helper()
	parsed, err := newHTML().Parse([]byte(markup))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return parsed.Sections
}

func TestHTMLClaimsOnlyHTMLExtensions(t *testing.T) {
	s := newHTML()
	for _, path := range []string{"a.html", "a.htm", "a.xhtml", "A.HTML", "A.HtM", "A.XHTML"} {
		if !s.Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	for _, path := range []string{"a.md", "a.txt", "a.pdf", "a.docx", "a.xml", "noext"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q", path)
		}
	}
}

func TestParseBuildsHeadingPaths(t *testing.T) {
	sections := parseSections(t, `<html><body>
		<h1>Guide</h1>
		<h2>Payments</h2><p>Pay the invoice within 30 days.</p>
		<h2>Refunds</h2><p>Refunds take 5 days.</p>
	</body></html>`)

	assertSection(t, sections, []string{"Guide", "Payments"}, "Pay the invoice within 30 days.")
	assertSection(t, sections, []string{"Guide", "Refunds"}, "Refunds take 5 days.")
}

func TestParseTextBeforeFirstHeadingHasEmptyPath(t *testing.T) {
	sections := parseSections(t, `<body><p>Preamble text.</p><h1>Guide</h1><p>Body.</p></body>`)

	assertSection(t, sections, nil, "Preamble text.")
	assertSection(t, sections, []string{"Guide"}, "Body.")
}

func TestParseRepairsMalformedMarkup(t *testing.T) {
	// The parser is tolerant: unclosed tags and a missing body are repaired, not rejected.
	sections := parseSections(t, `<h1>Title</h1><p>Body text<div>More`)

	assertSection(t, sections, []string{"Title"}, "Body text\n\nMore")
}

func TestParseEmptyDocumentHasNoSections(t *testing.T) {
	if sections := parseSections(t, ""); len(sections) != 0 {
		t.Fatalf("expected no sections for empty input, got %#v", sections)
	}
}

func TestChunkTitlesUseHeadingPath(t *testing.T) {
	s := newHTML()
	parsed, err := s.Parse([]byte(`<body><h1>Guide</h1><h2>Payments</h2><p>Pay on time.</p></body>`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chunks, err := s.Chunk(storage.Document{AbsolutePath: "guide.html"}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if !hasTitle(chunks, "Guide > Payments") {
		t.Fatalf("no chunk titled with the heading path: %v", titles(chunks))
	}
}

// --- helpers ---

func assertSection(t *testing.T, sections []strategy.Section, path []string, body string) {
	t.Helper()
	for _, s := range sections {
		if equalPath(s.Path, path) && strings.TrimSpace(s.Body) == body {
			return
		}
	}
	t.Fatalf("no section path=%v body=%q; got %+v", path, body, sections)
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasTitle(chunks []storage.Chunk, title string) bool {
	for _, c := range chunks {
		if c.Title == title {
			return true
		}
	}
	return false
}

func titles(chunks []storage.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Title
	}
	return out
}
