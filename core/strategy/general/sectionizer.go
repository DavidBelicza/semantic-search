package general

import (
	"strings"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

// Sectionizer assembles sections from a stream of headings and body paragraphs, using the
// shared heading stack so each section carries its full heading path. Every format that has
// headings (Markdown, HTML, DOCX, PDF) feeds it the same way, so they all section identically.
type Sectionizer struct {
	stack        []textproc.HeadingEntry
	sections     []strategy.Section
	body         strings.Builder
	separator    string
	pending      string
	pendingLevel int
}

// NewSectionizer builds a Sectionizer that joins the body paragraphs of one section with the
// given separator.
func NewSectionizer(separator string) *Sectionizer {
	return &Sectionizer{separator: separator}
}

// AddHeading opens a new section at the given level. Empty heading text is ignored.
func (s *Sectionizer) AddHeading(level int, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}

	s.close(level)
	s.stack = textproc.PushHeading(s.stack, level, text)
	s.pending = text
	s.pendingLevel = level
}

// AddBody appends a paragraph to the open section. Empty text is ignored.
func (s *Sectionizer) AddBody(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}

	s.pending = ""
	if s.body.Len() > 0 {
		s.body.WriteString(s.separator)
	}
	s.body.WriteString(text)
}

// Sections closes the open section and returns everything assembled so far.
func (s *Sectionizer) Sections() []strategy.Section {
	s.close(1)
	return s.sections
}

// close ends the open section. A heading that never received body text is emitted on its own
// rather than dropped, so a document whose prose sits in its headings is still indexed; a
// heading that only introduces deeper ones is left to its children.
func (s *Sectionizer) close(level int) {
	body := strings.TrimSpace(s.body.String())
	s.body.Reset()
	if body != "" {
		s.emit(body)
		return
	}
	if s.pending != "" && level <= s.pendingLevel {
		s.emit(s.pending)
	}

	s.pending = ""
}

func (s *Sectionizer) emit(body string) {
	s.sections = append(s.sections, strategy.Section{Path: textproc.PathOf(s.stack), Body: body})
}
