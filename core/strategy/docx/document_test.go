package docx

import (
	"archive/zip"
	"bytes"
	"testing"
)

// docxWithParts builds a .docx from an explicit set of parts, so a test can omit styles.xml or
// supply malformed XML.
func docxWithParts(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, body := range parts {
		writeZipPart(t, zw, name, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// TestParseWithoutStylesPart covers readStyleLevels' missing-styles branch: the document has no
// word/styles.xml, so heading levels come only from direct outlineLvl values.
func TestParseWithoutStylesPart(t *testing.T) {
	heading := `<w:p><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:r><w:t>Intro</w:t></w:r></w:p>`
	doc := document(heading + para("", "Opening text."))

	parsed, err := newDocx().Parse(docxWithParts(t, map[string]string{"word/document.xml": doc}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	assertSection(t, parsed.Sections, []string{"Intro"}, "Opening text.")
}

// TestParseRejectsMalformedDocumentXML covers sectionizeDocument's non-EOF decode-error branch.
func TestParseRejectsMalformedDocumentXML(t *testing.T) {
	content := docxWithParts(t, map[string]string{
		"word/document.xml": `<w:document ` + wNS + `><w:body><w:p><w:r><w:t>oops`,
		"word/styles.xml":   headingStyles(),
	})
	if _, err := newDocx().Parse(content); err == nil {
		t.Fatal("expected a decode error for malformed document.xml")
	}
}
