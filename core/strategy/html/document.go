package html

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

// extractSections parses the markup and turns its heading-structured content into sections.
func extractSections(content []byte) ([]strategy.Section, error) {
	document, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	walk := &walker{}
	walk.node(contentRoot(document))

	return sectionsFromBlocks(walk.blocks), nil
}

// contentRoot picks the subtree worth indexing: <main> when present, else a lone <article>,
// else the body. A page with several articles is a listing, not a single document, so its
// first article is not the content root.
func contentRoot(document *html.Node) *html.Node {
	if main := findElement(document, "main"); main != nil {
		return main
	}
	if articles := findElements(document, "article"); len(articles) == 1 {
		return articles[0]
	}
	if body := findElement(document, "body"); body != nil {
		return body
	}

	return document
}

func findElements(node *html.Node, name string) []*html.Node {
	var found []*html.Node
	if elementName(node) == name {
		found = append(found, node)
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		found = append(found, findElements(child, name)...)
	}

	return found
}

func findElement(node *html.Node, name string) *html.Node {
	if elementName(node) == name {
		return node
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}

	return nil
}

// block is one emitted piece of the document in reading order: a heading (level 1-6) or a
// body paragraph (level 0).
type block struct {
	level int
	text  string
}

// walker flattens the node tree into blocks, accumulating inline text in a buffer that is
// flushed at every block boundary.
type walker struct {
	blocks []block
	inline strings.Builder
}

func (w *walker) node(node *html.Node) {
	switch node.Type {
	case html.TextNode:
		w.inline.WriteString(node.Data)
	case html.ElementNode:
		w.element(node)
	case html.DocumentNode:
		w.children(node)
	}
}

func (w *walker) element(node *html.Node) {
	name := elementName(node)
	if isSkipped(name) {
		return
	}
	if name == "br" {
		w.flush()
		return
	}
	if name == "pre" {
		w.preformatted(node)
		return
	}
	if level := headingLevel(name); level > 0 {
		w.heading(node, level)
		return
	}

	w.container(node, isBlock(name))
}

func (w *walker) container(node *html.Node, block bool) {
	if block {
		w.flush()
	}

	w.children(node)

	if block {
		w.flush()
	}
}

func (w *walker) children(node *html.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.node(child)
	}
}

func (w *walker) heading(node *html.Node, level int) {
	w.flush()

	text := collapseSpaces(textOf(node))
	if text == "" {
		return
	}

	w.blocks = append(w.blocks, block{level: level, text: text})
}

// preformatted emits a <pre> verbatim: its whitespace is significant, so it bypasses the
// inline buffer and the space collapsing.
func (w *walker) preformatted(node *html.Node) {
	w.flush()

	text := textproc.TrimBlankLines(textOf(node))
	if strings.TrimSpace(text) == "" {
		return
	}

	w.blocks = append(w.blocks, block{text: text})
}

func (w *walker) flush() {
	text := collapseSpaces(w.inline.String())
	w.inline.Reset()
	if text == "" {
		return
	}

	w.blocks = append(w.blocks, block{text: text})
}

func textOf(node *html.Node) string {
	var out strings.Builder
	collectText(node, &out)

	return out.String()
}

func collectText(node *html.Node, out *strings.Builder) {
	if node.Type == html.TextNode {
		out.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode || isSkipped(elementName(node)) {
		return
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectText(child, out)
	}
}

func sectionsFromBlocks(blocks []block) []strategy.Section {
	sections := &sectionizer{}
	for _, item := range blocks {
		if item.level > 0 {
			sections.addHeading(item.level, item.text)
			continue
		}
		sections.addBody(item.text)
	}

	return sections.result()
}

// sectionizer assembles sections from a stream of headings and body paragraphs, using the
// shared heading stack so each section carries its full heading path.
type sectionizer struct {
	stack        []textproc.HeadingEntry
	sections     []strategy.Section
	body         strings.Builder
	pending      string
	pendingLevel int
}

func (s *sectionizer) addHeading(level int, text string) {
	s.close(level)
	s.stack = textproc.PushHeading(s.stack, level, text)
	s.pending = text
	s.pendingLevel = level
}

func (s *sectionizer) addBody(text string) {
	s.pending = ""
	if s.body.Len() > 0 {
		s.body.WriteString("\n\n")
	}
	s.body.WriteString(text)
}

// close ends the open section. A heading that never received body text is emitted on its own
// rather than dropped, so a page whose prose sits in its headings is still indexed; a heading
// that only introduces deeper ones is left to its children.
func (s *sectionizer) close(level int) {
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

func (s *sectionizer) emit(body string) {
	s.sections = append(s.sections, strategy.Section{Path: textproc.PathOf(s.stack), Body: body})
}

func (s *sectionizer) result() []strategy.Section {
	s.close(1)
	return s.sections
}
