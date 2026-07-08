package docx

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// parseStyleLevels reads word/styles.xml and returns each paragraph style's heading level
// (1-based), keyed by style id. Non-heading styles are absent. A style is a heading if it
// carries an outlineLvl (the reliable signal) or, failing that, is named "heading N".
func parseStyleLevels(r io.Reader) map[string]int {
	levels := map[string]int{}
	decoder := xml.NewDecoder(r)
	current := styleAccumulator{}

	for {
		tok, err := decoder.Token()
		if err != nil {
			return levels
		}
		current.consume(tok, levels)
	}
}

// styleAccumulator gathers one <w:style> element's id, name, and outline level as the decoder
// streams through it, committing a level on the closing tag.
type styleAccumulator struct {
	id      string
	name    string
	outline int
}

func (s *styleAccumulator) consume(tok xml.Token, levels map[string]int) {
	switch t := tok.(type) {
	case xml.StartElement:
		s.start(t)
	case xml.EndElement:
		s.end(t, levels)
	}
}

func (s *styleAccumulator) start(t xml.StartElement) {
	switch t.Name.Local {
	case "style":
		*s = styleAccumulator{outline: -1, id: attrValue(t.Attr, "styleId")}
	case "name":
		s.name = attrValue(t.Attr, "val")
	case "outlineLvl":
		s.outline = atoiOr(attrValue(t.Attr, "val"), -1)
	}
}

func (s *styleAccumulator) end(t xml.EndElement, levels map[string]int) {
	if t.Name.Local != "style" || s.id == "" {
		return
	}

	if level := styleLevel(s.outline, s.name); level > 0 {
		levels[s.id] = level
	}
}

// styleLevel resolves a heading level from an outline level (0-based → 1-based) or, when
// absent, from a "heading N" style name.
func styleLevel(outline int, name string) int {
	if outline >= 0 {
		return outline + 1
	}

	return headingNameLevel(name)
}

func headingNameLevel(name string) int {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(normalized, "heading ") {
		return 0
	}

	level, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(normalized, "heading ")))
	if err != nil || level < 1 {
		return 0
	}

	return level
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return attr.Value
		}
	}

	return ""
}

func atoiOr(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}

	return n
}
