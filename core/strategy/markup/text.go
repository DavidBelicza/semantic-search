package markup

import (
	"strings"

	"golang.org/x/net/html"
)

const paragraphSeparator = "\n\n"

var alwaysSkipped = map[string]struct{}{
	"script":   {},
	"style":    {},
	"noscript": {},
	"template": {},
	"canvas":   {},
	"head":     {},
	"nav":      {},
}

var webOnlySkipped = map[string]struct{}{
	"svg":    {},
	"footer": {},
	"aside":  {},
}

var standardBlocks = map[string]struct{}{
	"address": {}, "article": {}, "aside": {}, "blockquote": {}, "details": {},
	"dd": {}, "div": {}, "dl": {}, "dt": {}, "fieldset": {}, "figcaption": {},
	"figure": {}, "footer": {}, "form": {}, "header": {}, "hr": {}, "li": {},
	"main": {}, "nav": {}, "ol": {}, "p": {}, "pre": {}, "section": {},
	"table": {}, "tbody": {}, "td": {}, "tfoot": {}, "th": {}, "thead": {},
	"tr": {}, "ul": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
}

var headingLevels = map[string]int{
	"h1": 1, "h2": 2, "h3": 3, "h4": 4, "h5": 5, "h6": 6,
}

// IsSkipped reports whether an element is excluded by the selected mode.
func IsSkipped(name string, mode Mode) bool {
	if _, ok := alwaysSkipped[name]; ok {
		return true
	}
	if mode == PublicationMode {
		return false
	}

	_, ok := webOnlySkipped[name]
	return ok
}

// IsBlock reports the static block classification for an element name. Publication SVG text
// receives additional ancestry-aware handling inside the walker.
func IsBlock(name string, mode Mode) bool {
	if _, ok := standardBlocks[name]; ok {
		return true
	}

	return mode == PublicationMode && name == "svg"
}

func (m Mode) isBlockNode(node *html.Node) bool {
	name := ElementName(node)
	if IsBlock(name, m) {
		return true
	}

	return m == PublicationMode && name == "text" && hasAncestorElement(node, "svg")
}

// HeadingLevel returns the HTML heading level or zero for a non-heading element.
func HeadingLevel(name string) int {
	return headingLevels[name]
}

// ElementName returns a lower-cased element name and ignores other node kinds.
func ElementName(node *html.Node) string {
	if node == nil || node.Type != html.ElementNode {
		return ""
	}

	return strings.ToLower(node.Data)
}

func (m Mode) skipsNode(node *html.Node) bool {
	if IsSkipped(ElementName(node), m) {
		return true
	}
	if m != PublicationMode {
		return false
	}
	if hasElementAttribute(node, "hidden") || elementAttributeEquals(node, "aria-hidden", "true") {
		return true
	}
	if elementAttributeHasToken(node, "epub:type", "pagebreak") || elementAttributeHasToken(node, "role", "doc-pagebreak") {
		return true
	}

	return ElementName(node) == "title" && hasAncestorElement(node, "svg")
}

func (m Mode) replacementText(node *html.Node) (string, bool) {
	if m != PublicationMode || ElementName(node) != "img" {
		return "", false
	}

	alt, _ := elementAttribute(node, "alt")
	return alt, true
}

func elementAttribute(node *html.Node, name string) (string, bool) {
	for _, attr := range node.Attr {
		qualified := attr.Key
		if attr.Namespace != "" {
			qualified = attr.Namespace + ":" + attr.Key
		}
		if qualified == name || attr.Key == name {
			return attr.Val, true
		}
	}

	return "", false
}

func hasElementAttribute(node *html.Node, name string) bool {
	_, ok := elementAttribute(node, name)

	return ok
}

func elementAttributeEquals(node *html.Node, name, wanted string) bool {
	value, ok := elementAttribute(node, name)

	return ok && strings.EqualFold(strings.TrimSpace(value), wanted)
}

func elementAttributeHasToken(node *html.Node, name, wanted string) bool {
	value, ok := elementAttribute(node, name)
	if !ok {
		return false
	}

	for _, token := range strings.Fields(value) {
		if token == wanted {
			return true
		}
	}

	return false
}

func hasAncestorElement(node *html.Node, name string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if ElementName(parent) == name {
			return true
		}
	}

	return false
}

// CollapseSpaces normalizes markup whitespace runs to a single ASCII space.
func CollapseSpaces(text string) string {
	return strings.Join(strings.FieldsFunc(text, IsHTMLSpace), " ")
}

// IsHTMLSpace reports the whitespace characters normalized by CollapseSpaces.
func IsHTMLSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', ' ':
		return true
	default:
		return false
	}
}
