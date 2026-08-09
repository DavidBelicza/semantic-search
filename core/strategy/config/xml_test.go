package config

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestParseXMLNestsElementsAndFoldsAttributes(t *testing.T) {
	got := render(t, parseXML, `<config><!-- the primary database --><db name="primary"><host>db01</host></db></config>`)

	want := strings.Join([]string{
		"config:",
		"  # the primary database",
		"  db:",
		"    @name = primary",
		"    host = db01",
	}, "\n")

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseXMLKeepsTextOfElementWithAttributes(t *testing.T) {
	got := render(t, parseXML, `<a href="/x">click here</a>`)

	want := "a:\n  @href = /x\n  - click here"
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

func TestCloseXMLElementNeverPopsPastTheRoot(t *testing.T) {
	stack := closeXMLElement([]node{{Key: "root"}})

	if len(stack) != 1 || stack[0].Key != "root" {
		t.Fatalf("got %+v", stack)
	}
}

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
