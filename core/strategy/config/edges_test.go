package config

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestParseJSONReportsAnEmptySource(t *testing.T) {
	if _, err := parseJSON(""); err == nil {
		t.Fatal("want an error for an empty source")
	}
}

func TestParseJSONReportsATruncatedArray(t *testing.T) {
	if _, err := parseJSON(`[1,`); err == nil {
		t.Fatal("want an error for a truncated array")
	}
	if _, err := parseJSON(`{"a":[{"b":`); err == nil {
		t.Fatal("want an error for a truncated object inside an array")
	}
}

func TestJSONScalarRendersAnUnexpectedTokenAsEmpty(t *testing.T) {
	if got := jsonScalar(json.Token(struct{}{})); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParseJSONNestsArraysOfObjects(t *testing.T) {
	got := render(t, parseJSON, `{"users":[{"name":"ada"},{"name":"alan"}]}`)

	want := "users:\n  -\n    name = ada\n  -\n    name = alan"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseXMLReportsInvalidEncoding(t *testing.T) {
	if _, err := parseXML("<a>\xff\xfe</a>"); err == nil {
		t.Fatal("want an error for invalid UTF-8")
	}
}

func TestParseXMLIgnoresDirectivesAndProcessingInstructions(t *testing.T) {
	got := render(t, parseXML, `<?xml version="1.0"?><!DOCTYPE cfg><cfg><a>1</a></cfg>`)

	if got != "cfg:\n  a = 1" {
		t.Fatalf("got: %q", got)
	}
}

// The decoder rejects a stray end tag before the walk sees it, so the root guard is asserted
// directly: it must never pop past the root.
func TestCloseXMLElementNeverPopsPastTheRoot(t *testing.T) {
	stack := closeXMLElement([]node{{Key: "root"}})

	if len(stack) != 1 || stack[0].Key != "root" {
		t.Fatalf("got %+v", stack)
	}
}

// A file the decoder rejects is still indexed, as text, rather than dropped.
func TestConfigIndexesXMLThatDoesNotParse(t *testing.T) {
	chunks := chunksOf(t, "/p/broken.xml", `<a>1</a></a>`)

	if len(chunks) == 0 || !strings.Contains(joinedText(chunks), "<a>1</a>") {
		t.Fatalf("want the raw text kept, got: %s", joinedText(chunks))
	}
}

func TestAppendXMLTextJoinsSplitCharacterData(t *testing.T) {
	stack := appendXMLText([]node{{}, {Key: "a"}}, "one")
	stack = appendXMLText(stack, "  ")
	stack = appendXMLText(stack, "two")

	if stack[1].Value != "one two" {
		t.Fatalf("got %q", stack[1].Value)
	}
}

func TestConsumeXMLTokenKeepsAPendingCommentAcrossText(t *testing.T) {
	stack, comment := consumeXMLToken([]node{{}}, xml.CharData("  "), "note")

	if comment != "note" {
		t.Fatalf("comment lost: %q", comment)
	}
	if len(stack) != 1 {
		t.Fatalf("stack changed: %d", len(stack))
	}
}

func TestParseYAMLHandlesSequencesOfScalars(t *testing.T) {
	got := render(t, parseYAML, "hosts:\n  - db01\n  - db02\n")

	if got != "hosts:\n  - db01\n  - db02" {
		t.Fatalf("got: %q", got)
	}
}

func TestParseYAMLHandlesAnEmptySource(t *testing.T) {
	got, err := parseYAML("")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Children) != 0 {
		t.Fatalf("want an empty tree, got %d children", len(got.Children))
	}
}

func TestYAMLDocumentHandlesAnEmptyDocumentNode(t *testing.T) {
	if got := yamlDocument(yaml.Node{}); len(got.Children) != 0 || got.Value != "" {
		t.Fatalf("want an empty node, got %+v", got)
	}
}

func TestYAMLAliasWithoutATargetYieldsTheKeyAlone(t *testing.T) {
	got := yamlAlias("prod", &yaml.Node{Kind: yaml.AliasNode})

	if got.Key != "prod" || len(got.Children) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseINIJoinsConsecutiveCommentLines(t *testing.T) {
	got := render(t, parseINI, "# first\n# second\nkey = value\n")

	if got != "# first\n# second\nkey = value" {
		t.Fatalf("got: %q", got)
	}
}

// A blank line ends a comment block, so it does not attach to a distant key.
func TestParseINIDropsACommentSeparatedByABlankLine(t *testing.T) {
	got := render(t, parseINI, "# orphan\n\nkey = value\n")

	if got != "key = value" {
		t.Fatalf("got: %q", got)
	}
}

func TestSplitKeyPathFallsBackToTheWholeKey(t *testing.T) {
	got := splitKeyPath("..")

	if len(got) != 1 || got[0] != ".." {
		t.Fatalf("got %v", got)
	}
}

func TestInsertPathReusesAnExistingBranch(t *testing.T) {
	root := insertPath(node{}, []string{"a", "b"}, node{Key: "x", Value: "1"})
	root = insertPath(root, []string{"a", "b"}, node{Key: "y", Value: "2"})

	if len(root.Children) != 1 {
		t.Fatalf("want one top branch, got %d", len(root.Children))
	}
	if got := renderSubtree(root); got != "a:\n  b:\n    x = 1\n    y = 2" {
		t.Fatalf("got: %q", got)
	}
}

// A key used as both a value and a prefix must not have the leaf mistaken for the branch.
func TestInsertPathDoesNotReuseALeafAsABranch(t *testing.T) {
	root := insertPath(node{}, nil, node{Key: "a", Value: "1"})
	root = insertPath(root, []string{"a"}, node{Key: "b", Value: "2"})

	if got := renderSubtree(root); got != "a = 1\na:\n  b = 2" {
		t.Fatalf("got: %q", got)
	}
}

// The banner scan is bounded, so a marker far down the file does not count.
func TestIsGeneratedContentOnlyScansTheLeadingLines(t *testing.T) {
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines, "key = value")
	}
	lines = append(lines, "# code generated by tool")

	if isGeneratedContent(strings.Join(lines, "\n")) {
		t.Fatal("a marker past the scan window should not count")
	}
	if !isGeneratedContent("# @generated\nkey = value") {
		t.Fatal("a marker in the leading lines should count")
	}
}

func TestFlatSectionsAndWholeSectionIgnoreEmptyInput(t *testing.T) {
	if got := flatSections("   \n\n  "); got != nil {
		t.Fatalf("want no sections, got %v", got)
	}
	if got := wholeSection(""); got != nil {
		t.Fatalf("want no parts, got %v", got)
	}
}

// An extension with no parser cannot arrive through Claims, but the fallback keeps the file
// indexed rather than dropping it if one ever does.
func TestSectionsOfFallsBackForAnUnknownExtension(t *testing.T) {
	strategy := configStrategy{maxTokens: 350}

	sections := strategy.sectionsOf("/p/a.unknown", "host = db01")

	if len(sections) != 1 || !strings.Contains(sections[0].Body, "db01") {
		t.Fatalf("got %v", sections)
	}
}

func TestSectionsOfFallsBackWhenTheTreeYieldsNothing(t *testing.T) {
	strategy := configStrategy{maxTokens: 350}

	sections := strategy.sectionsOf("/p/a.ini", "no entries here")

	if len(sections) != 1 || !strings.Contains(sections[0].Body, "no entries here") {
		t.Fatalf("got %v", sections)
	}
}

func TestRedactionLeavesOrdinaryKeysAlone(t *testing.T) {
	for _, key := range []string{"password", "API_KEY", "client_secret", "auth-token", "db.pwd"} {
		if !isSecretKey(key) {
			t.Fatalf("%q should be treated as a credential", key)
		}
	}
	for _, key := range []string{"host", "port", "keyboard", "public_url", "timeout"} {
		if isSecretKey(key) {
			t.Fatalf("%q should not be treated as a credential", key)
		}
	}
}
