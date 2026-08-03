package html

import (
	"testing"

	"golang.org/x/net/html"
)

func TestElementClassification(t *testing.T) {
	if !isSkipped("script") || isSkipped("p") {
		t.Fatal("skipped classification mismatch")
	}
	if !isBlock("p") || isBlock("span") {
		t.Fatal("block classification mismatch")
	}
	if headingLevel("h3") != 3 || headingLevel("p") != 0 {
		t.Fatal("heading level mismatch")
	}
}

func TestElementNameLowercasesAndIgnoresNonElements(t *testing.T) {
	if got := elementName(&html.Node{Type: html.ElementNode, Data: "DIV"}); got != "div" {
		t.Fatalf("expected a lowercased tag name, got %q", got)
	}
	if got := elementName(&html.Node{Type: html.TextNode, Data: "text"}); got != "" {
		t.Fatalf("expected an empty name for a non-element node, got %q", got)
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"  a   b  ":    "a b",
		"a\t\n\r\v\fb": "a b",
		"a b":          "a b",
		"":             "",
		"   ":          "",
		"single":       "single",
		"trailing   ":  "trailing",
	}
	for input, want := range cases {
		if got := collapseSpaces(input); got != want {
			t.Fatalf("collapseSpaces(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsHTMLSpace(t *testing.T) {
	for _, r := range []rune{' ', '\t', '\n', '\r', '\f', '\v', ' '} {
		if !isHTMLSpace(r) {
			t.Fatalf("%q should be whitespace", r)
		}
	}
	for _, r := range []rune{'a', '-', '0'} {
		if isHTMLSpace(r) {
			t.Fatalf("%q should not be whitespace", r)
		}
	}
}
