package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

// makeDocx builds a minimal .docx (a ZIP of document.xml + styles.xml) in memory.
func makeDocx(t *testing.T, documentXML, stylesXML string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipPart(t, zw, "word/document.xml", documentXML)
	writeZipPart(t, zw, "word/styles.xml", stylesXML)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func writeZipPart(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func para(styleID, text string) string {
	style := ""
	if styleID != "" {
		style = `<w:pPr><w:pStyle w:val="` + styleID + `"/></w:pPr>`
	}
	return `<w:p>` + style + `<w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func document(body string) string {
	return `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`
}

func headingStyles() string {
	return `<w:styles ` + wNS + `>` +
		`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>` +
		`</w:styles>`
}

func newDocx() strategy.Strategy {
	return NewDocxStrategy(general.NewGeneralStrategy(nil, nil))
}

func TestDocxClaimsOnlyDocx(t *testing.T) {
	s := newDocx()
	for _, path := range []string{"a.docx", "A.DOCX"} {
		if !s.Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	for _, path := range []string{"a.doc", "a.md", "a.pdf", "a.txt"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q", path)
		}
	}
}

func TestParseBuildsHeadingPaths(t *testing.T) {
	body := para("Heading1", "Guide") +
		para("Heading2", "Payments") +
		para("", "Pay the invoice within 30 days.") +
		para("Heading2", "Refunds") +
		para("", "Refunds take 5 days.")
	content := makeDocx(t, document(body), headingStyles())

	parsed, err := newDocx().Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	assertSection(t, parsed.Sections, []string{"Guide", "Payments"}, "Pay the invoice within 30 days.")
	assertSection(t, parsed.Sections, []string{"Guide", "Refunds"}, "Refunds take 5 days.")
}

func TestParseUsesDirectOutlineWithoutStyles(t *testing.T) {
	// No styles.xml heading definitions; the paragraph declares its level directly.
	heading := `<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>Intro</w:t></w:r></w:p>`
	body := heading + para("", "Some opening text.")
	content := makeDocx(t, document(body), `<w:styles `+wNS+`></w:styles>`)

	parsed, err := newDocx().Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	assertSection(t, parsed.Sections, []string{"Intro"}, "Some opening text.")
}

func TestParseTextBeforeFirstHeadingHasEmptyPath(t *testing.T) {
	body := para("", "Preamble text.") + para("Heading1", "Guide") + para("", "Body.")
	content := makeDocx(t, document(body), headingStyles())

	parsed, err := newDocx().Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	assertSection(t, parsed.Sections, nil, "Preamble text.")
}

func TestChunkTitlesUseHeadingPath(t *testing.T) {
	body := para("Heading1", "Guide") + para("Heading2", "Payments") + para("", "Pay on time.")
	content := makeDocx(t, document(body), headingStyles())
	s := newDocx()

	parsed, err := s.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := s.Chunk(storage.Document{AbsolutePath: "guide.docx"}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if !hasTitle(chunks, "Guide > Payments") {
		t.Fatalf("no chunk titled with heading path: %v", titles(chunks))
	}
}

func TestParseRejectsNonZip(t *testing.T) {
	if _, err := newDocx().Parse([]byte("not a zip")); err == nil {
		t.Fatal("expected error for non-zip content")
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

func TestParseUsesStyleNameLevelWithoutOutline(t *testing.T) {
	styles := `<w:styles ` + wNS + `><w:style w:type="paragraph" w:styleId="H3"><w:name w:val="heading 3"/></w:style></w:styles>`
	doc := document(para("H3", "Deep Heading") + para("", "Body text under the heading."))

	parsed, err := newDocx().Parse(makeDocx(t, doc, styles))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	found := false
	for _, s := range parsed.Sections {
		if len(s.Path) > 0 && s.Path[len(s.Path)-1] == "Deep Heading" && strings.Contains(s.Body, "Body text") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a heading-name-based section, got %#v", parsed.Sections)
	}
}

func TestParseRejectsMissingDocumentPart(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipPart(t, zw, "word/styles.xml", headingStyles()) // no word/document.xml
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	if _, err := newDocx().Parse(buf.Bytes()); err == nil {
		t.Fatal("expected an error when word/document.xml is missing")
	}
}

func TestParseHandlesTabsBlanksAndConsecutiveBodies(t *testing.T) {
	// Newlines between elements exercise chars() while not capturing; <w:tab/> exercises the tab
	// case; an empty heading and empty body hit the early returns; two bodies in a row hit the
	// append branch; a non-numeric outlineLvl exercises the atoiOr fallback.
	body := para("Heading1", "") +
		para("", "") +
		`<w:p><w:pPr><w:outlineLvl w:val="notanumber"/></w:pPr><w:r><w:t>First</w:t><w:tab/><w:t>line</w:t></w:r></w:p>` +
		para("", "Second body paragraph.")
	doc := "<w:document " + wNS + ">\n  <w:body>\n    " + body + "\n  </w:body>\n</w:document>"

	parsed, err := newDocx().Parse(makeDocx(t, doc, headingStyles()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) == 0 {
		t.Fatalf("expected at least one section, got %#v", parsed.Sections)
	}
}
