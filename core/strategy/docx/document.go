package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

const (
	documentPart = "word/document.xml"
	stylesPart   = "word/styles.xml"
)

// extractSections opens the .docx container, reads its style heading levels, and turns the
// document body into structured sections.
func extractSections(content []byte) ([]strategy.Section, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}

	levels := readStyleLevels(reader)

	document, err := openZipPart(reader, documentPart)
	if err != nil {
		return nil, err
	}
	defer document.Close()

	return sectionizeDocument(document, levels)
}

// readStyleLevels loads styles.xml; a missing or unreadable styles part is not fatal, since
// paragraphs can still declare a heading level directly via outlineLvl.
func readStyleLevels(reader *zip.Reader) map[string]int {
	styles, err := openZipPart(reader, stylesPart)
	if err != nil {
		return map[string]int{}
	}
	defer styles.Close()

	return parseStyleLevels(styles)
}

func openZipPart(reader *zip.Reader, name string) (io.ReadCloser, error) {
	for _, file := range reader.File {
		if file.Name == name {
			return file.Open()
		}
	}

	return nil, fmt.Errorf("docx: missing %s", name)
}

// sectionizeDocument streams document.xml, classifying each paragraph as a heading (which
// updates the path) or body text (which fills the current section).
func sectionizeDocument(r io.Reader, levels map[string]int) ([]strategy.Section, error) {
	decoder := xml.NewDecoder(r)
	sections := general.NewSectionizer(paragraphSeparator)
	para := paragraph{outline: -1}

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return sections.Sections(), nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", documentPart, err)
		}
		para.consume(tok, sections, levels)
	}
}

// paragraph accumulates one <w:p>: its style id, direct outline level, and text, capturing
// <w:t> character data in document order (so runs and hyperlinks keep their order).
type paragraph struct {
	text      strings.Builder
	style     string
	outline   int
	capturing bool
}

func (p *paragraph) consume(tok xml.Token, sections *general.Sectionizer, levels map[string]int) {
	switch t := tok.(type) {
	case xml.StartElement:
		p.start(t)
	case xml.CharData:
		p.chars(t)
	case xml.EndElement:
		p.end(t, sections, levels)
	}
}

func (p *paragraph) start(t xml.StartElement) {
	switch t.Name.Local {
	case "p":
		*p = paragraph{outline: -1}
	case "pStyle":
		p.style = attrValue(t.Attr, "val")
	case "outlineLvl":
		p.outline = atoiOr(attrValue(t.Attr, "val"), -1)
	case "t":
		p.capturing = true
	case "tab":
		p.text.WriteString(" ")
	}
}

func (p *paragraph) chars(data xml.CharData) {
	if !p.capturing {
		return
	}

	p.text.Write(data)
}

func (p *paragraph) end(t xml.EndElement, sections *general.Sectionizer, levels map[string]int) {
	switch t.Name.Local {
	case "t":
		p.capturing = false
	case "p":
		p.commit(sections, levels)
	}
}

func (p *paragraph) commit(sections *general.Sectionizer, levels map[string]int) {
	text := strings.TrimSpace(p.text.String())
	level := p.level(levels)

	if level > 0 {
		sections.AddHeading(level, text)
		return
	}

	sections.AddBody(text)
}

// level is the paragraph's heading level: a direct outlineLvl wins, else its style's level,
// else 0 (body text).
func (p *paragraph) level(levels map[string]int) int {
	if p.outline >= 0 {
		return p.outline + 1
	}

	return levels[p.style]
}

// paragraphSeparator joins the paragraphs of one section.
const paragraphSeparator = "\n\n"
