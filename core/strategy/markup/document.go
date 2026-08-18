// Package markup extracts heading-structured prose from HTML-family documents. It provides
// web-page and publication modes so concrete strategies can share one tolerant parser without
// depending on each other.
package markup

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

// Mode selects the extraction policy. The policy derives content-root selection, skipped
// elements, publication metadata, image alternatives, and SVG block handling from one value.
type Mode uint8

const (
	// WebMode extracts the principal prose of an ordinary web page.
	WebMode Mode = iota
	// PublicationMode extracts the complete readable body of an EPUB content document.
	PublicationMode
)

// Document is the readable structure extracted from one markup document.
type Document struct {
	Title    string
	Sections []strategy.Section
}

// Block is one heading or body fragment emitted while walking a document.
type Block struct {
	Level int
	Text  string
}

// Extract parses markup with the tolerant HTML parser and applies the selected extraction mode.
func Extract(r io.Reader, mode Mode) (Document, error) {
	document, err := html.Parse(r)
	if err != nil {
		return Document{}, fmt.Errorf("parse html: %w", err)
	}

	blocks := Blocks(ContentRoot(document, mode), mode)

	return Document{
		Title:    documentTitle(document, mode),
		Sections: sectionsFromBlocks(blocks),
	}, nil
}

// ContentRoot selects the subtree appropriate to the mode. Publication content documents are
// already publication units, while web pages need their main article isolated from furniture.
func ContentRoot(document *html.Node, mode Mode) *html.Node {
	if mode == PublicationMode {
		return publicationContentRoot(document)
	}

	return webContentRoot(document)
}

func publicationContentRoot(document *html.Node) *html.Node {
	if body := FindElement(document, "body"); body != nil {
		return body
	}

	return document
}

func webContentRoot(document *html.Node) *html.Node {
	if main := FindElement(document, "main"); main != nil {
		return main
	}
	if articles := FindElements(document, "article"); len(articles) == 1 {
		return articles[0]
	}
	if body := FindElement(document, "body"); body != nil {
		return body
	}

	return document
}

// FindElements returns every element with the given local name in document order.
func FindElements(node *html.Node, name string) []*html.Node {
	var found []*html.Node
	if ElementName(node) == name {
		found = append(found, node)
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		found = append(found, FindElements(child, name)...)
	}

	return found
}

// FindElement returns the first element with the given local name.
func FindElement(node *html.Node, name string) *html.Node {
	if ElementName(node) == name {
		return node
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := FindElement(child, name); found != nil {
			return found
		}
	}

	return nil
}

// Blocks walks a parsed subtree and returns its readable fragments in document order.
func Blocks(root *html.Node, mode Mode) []Block {
	walk := &walker{mode: mode}
	walk.node(root)
	walk.flush()

	return walk.blocks
}

type walker struct {
	mode   Mode
	blocks []Block
	inline strings.Builder
}

func (w *walker) node(node *html.Node) {
	if node == nil {
		return
	}

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
	if w.mode.skipsNode(node) {
		return
	}
	if replacement, ok := w.mode.replacementText(node); ok {
		w.inline.WriteString(replacement)
		return
	}

	name := ElementName(node)
	if name == "br" {
		w.flush()
		return
	}
	if name == "pre" {
		w.preformatted(node)
		return
	}
	if level := HeadingLevel(name); level > 0 {
		w.heading(node, level)
		return
	}

	w.container(node, w.mode.isBlockNode(node))
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

	text := CollapseSpaces(w.textOf(node))
	if text == "" {
		return
	}

	w.blocks = append(w.blocks, Block{Level: level, Text: text})
}

func (w *walker) preformatted(node *html.Node) {
	w.flush()

	text := textproc.TrimBlankLines(w.textOf(node))
	if strings.TrimSpace(text) == "" {
		return
	}

	w.blocks = append(w.blocks, Block{Text: text})
}

func (w *walker) flush() {
	text := CollapseSpaces(w.inline.String())
	w.inline.Reset()
	if text == "" {
		return
	}

	w.blocks = append(w.blocks, Block{Text: text})
}

// textOf uses the same recursive collector for headings, preformatted blocks, and titles. The
// root is already selected by its caller, so only descendants are filtered.
func (w *walker) textOf(node *html.Node) string {
	var out strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.collectText(child, &out)
	}

	return out.String()
}

func (w *walker) collectText(node *html.Node, out *strings.Builder) {
	if node.Type == html.TextNode {
		out.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode || w.mode.skipsNode(node) {
		return
	}
	if replacement, ok := w.mode.replacementText(node); ok {
		out.WriteString(replacement)
		return
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.collectText(child, out)
	}
}

func documentTitle(document *html.Node, mode Mode) string {
	if mode != PublicationMode {
		return ""
	}

	title := headTitle(document)
	if title == nil {
		title = standaloneSVGTitle(document)
	}
	if title == nil {
		return ""
	}

	walk := &walker{mode: mode}
	return CollapseSpaces(walk.textOf(title))
}

func headTitle(document *html.Node) *html.Node {
	head := FindElement(document, "head")
	if head == nil {
		return nil
	}

	return FindElement(head, "title")
}

// standaloneSVGTitle only treats a title as document metadata when the body contains one
// top-level SVG. Titles inside SVG figures embedded in XHTML remain figure metadata.
func standaloneSVGTitle(document *html.Node) *html.Node {
	body := FindElement(document, "body")
	if body == nil {
		return nil
	}

	var svg *html.Node
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if ElementName(child) != "svg" || svg != nil {
			return nil
		}
		svg = child
	}
	if svg == nil {
		return nil
	}

	return FindElement(svg, "title")
}

func sectionsFromBlocks(blocks []Block) []strategy.Section {
	sections := general.NewSectionizer(paragraphSeparator)
	for _, item := range blocks {
		if item.Level > 0 {
			sections.AddHeading(item.Level, item.Text)
			continue
		}
		sections.AddBody(item.Text)
	}

	return sections.Sections()
}
