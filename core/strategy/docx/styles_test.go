package docx

import "testing"

// TestStyleLevelNamesAndMissingAttrs covers headingNameLevel's non-heading and invalid-number
// branches and attrValue's not-found return via styles whose ids/vals are absent or odd. None of
// these styles resolve to a heading, so a paragraph using them stays body text.
func TestStyleLevelNamesAndMissingAttrs(t *testing.T) {
	styles := `<w:styles ` + wNS + `>` +
		`<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Zero"><w:name w:val="heading zero"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Low"><w:name w:val="heading 0"/></w:style>` +
		`<w:style w:type="paragraph"><w:name/></w:style>` + // no styleId, no name val
		`</w:styles>`
	doc := document(para("Normal", "Body under a non-heading style."))

	parsed, err := newDocx().Parse(docxWithParts(t, map[string]string{
		"word/document.xml": doc,
		"word/styles.xml":   styles,
	}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	assertSection(t, parsed.Sections, nil, "Body under a non-heading style.")
}
