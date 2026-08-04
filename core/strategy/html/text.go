package html

import (
	"strings"

	"golang.org/x/net/html"
)

// skipped elements never contribute prose: their text is code, styling, metadata, or page
// furniture repeated on every page. Their whole subtree is dropped.
var skipped = map[string]struct{}{
	"script":   {},
	"style":    {},
	"noscript": {},
	"template": {},
	"svg":      {},
	"canvas":   {},
	"head":     {},
	"nav":      {},
	"footer":   {},
	"aside":    {},
}

// blocks force a paragraph break around their content. Everything else is inline and
// concatenates without a space, so "<b>a</b><i>b</i>" stays "ab".
var blocks = map[string]struct{}{
	"address": {}, "article": {}, "aside": {}, "blockquote": {}, "details": {},
	"dd": {}, "div": {}, "dl": {}, "dt": {}, "fieldset": {}, "figcaption": {},
	"figure": {}, "footer": {}, "form": {}, "header": {}, "hr": {}, "li": {},
	"main": {}, "nav": {}, "ol": {}, "p": {}, "pre": {}, "section": {},
	"table": {}, "tbody": {}, "td": {}, "tfoot": {}, "th": {}, "thead": {},
	"tr": {}, "ul": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
}

// headingLevels maps a heading tag to its numeric level for the shared heading stack.
var headingLevels = map[string]int{
	"h1": 1, "h2": 2, "h3": 3, "h4": 4, "h5": 5, "h6": 6,
}

func isSkipped(name string) bool {
	_, ok := skipped[name]
	return ok
}

func isBlock(name string) bool {
	_, ok := blocks[name]
	return ok
}

func headingLevel(name string) int {
	return headingLevels[name]
}

func elementName(node *html.Node) string {
	if node.Type != html.ElementNode {
		return ""
	}

	return strings.ToLower(node.Data)
}

// collapseSpaces normalizes whitespace runs to a single space, folding the decoded
// non-breaking space (U+00A0) too so it does not survive as a look-alike character.
func collapseSpaces(text string) string {
	return strings.Join(strings.FieldsFunc(text, isHTMLSpace), " ")
}

func isHTMLSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', ' ':
		return true
	default:
		return false
	}
}
