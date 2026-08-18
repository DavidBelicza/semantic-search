package markup

import (
	"testing"

	"golang.org/x/net/html"
)

func TestIsSkippedPerMode(t *testing.T) {
	for _, name := range []string{"script", "style", "noscript", "template", "canvas", "head", "nav"} {
		if !IsSkipped(name, WebMode) || !IsSkipped(name, PublicationMode) {
			t.Fatalf("%q should be skipped in both modes", name)
		}
	}
	for _, name := range []string{"svg", "footer", "aside"} {
		if !IsSkipped(name, WebMode) {
			t.Fatalf("%q should be skipped for web pages", name)
		}
		if IsSkipped(name, PublicationMode) {
			t.Fatalf("%q should be kept in a publication", name)
		}
	}
	if IsSkipped("p", WebMode) || IsSkipped("p", PublicationMode) {
		t.Fatal("prose elements must never be skipped")
	}
}

func TestIsBlockPerMode(t *testing.T) {
	if !IsBlock("p", WebMode) || IsBlock("span", WebMode) {
		t.Fatal("standard block classification mismatch")
	}
	if IsBlock("svg", WebMode) {
		t.Fatal("svg is not a web block")
	}
	if !IsBlock("svg", PublicationMode) {
		t.Fatal("svg is a publication block")
	}
}

func TestIsBlockNodeHandlesSVGText(t *testing.T) {
	svg := &html.Node{Type: html.ElementNode, Data: "svg"}
	text := &html.Node{Type: html.ElementNode, Data: "text", Parent: svg}
	loose := &html.Node{Type: html.ElementNode, Data: "text"}

	if !PublicationMode.isBlockNode(text) {
		t.Fatal("text inside svg is a publication block")
	}
	if PublicationMode.isBlockNode(loose) {
		t.Fatal("text outside svg is not a block")
	}
	if WebMode.isBlockNode(text) {
		t.Fatal("svg text is never a web block")
	}
}

func TestHeadingLevel(t *testing.T) {
	if HeadingLevel("h3") != 3 || HeadingLevel("p") != 0 {
		t.Fatal("heading level mismatch")
	}
}

func TestElementNameLowercasesAndIgnoresNonElements(t *testing.T) {
	if got := ElementName(&html.Node{Type: html.ElementNode, Data: "DIV"}); got != "div" {
		t.Fatalf("expected a lowercased tag name, got %q", got)
	}
	if got := ElementName(&html.Node{Type: html.TextNode, Data: "text"}); got != "" {
		t.Fatalf("expected an empty name for a non-element node, got %q", got)
	}
	if got := ElementName(nil); got != "" {
		t.Fatalf("expected an empty name for a nil node, got %q", got)
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"  a   b  ":    "a b",
		"a\t\n\r\v\fb": "a b",
		"a b":          "a b",
		"":             "",
		"   ":          "",
		"single":       "single",
		"trailing   ":  "trailing",
	}
	for input, want := range cases {
		if got := CollapseSpaces(input); got != want {
			t.Fatalf("CollapseSpaces(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsHTMLSpace(t *testing.T) {
	for _, r := range []rune{' ', '\t', '\n', '\r', '\f', '\v', ' '} {
		if !IsHTMLSpace(r) {
			t.Fatalf("%q should be whitespace", r)
		}
	}
	for _, r := range []rune{'a', '-', '0'} {
		if IsHTMLSpace(r) {
			t.Fatalf("%q should not be whitespace", r)
		}
	}
}

func TestAttributeHelpers(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, Data: "p", Attr: []html.Attribute{
		{Key: "hidden"},
		{Key: "aria-hidden", Val: " TRUE "},
		{Namespace: "epub", Key: "type", Val: "chapter pagebreak"},
	}}

	if !hasElementAttribute(node, "hidden") || hasElementAttribute(node, "missing") {
		t.Fatal("attribute presence mismatch")
	}
	if !elementAttributeEquals(node, "aria-hidden", "true") {
		t.Fatal("attribute comparison should trim and ignore case")
	}
	if elementAttributeEquals(node, "missing", "true") {
		t.Fatal("absent attribute must not compare equal")
	}
	if !elementAttributeHasToken(node, "epub:type", "pagebreak") {
		t.Fatal("qualified attribute token not found")
	}
	if elementAttributeHasToken(node, "epub:type", "cover") || elementAttributeHasToken(node, "missing", "x") {
		t.Fatal("unexpected token match")
	}
}
