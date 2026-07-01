package parser

import (
	"context"
	"testing"
)

func TestMarkdownParserKeepsCleanTextUnchanged(t *testing.T) {
	got, err := (MarkdownParser{}).Parse(context.Background(), "# Title\n\nBody")
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}

	if got != "# Title\n\nBody" {
		t.Fatalf("parsed text mismatch: %q", got)
	}
}

func TestNormalizeMarkdownCleansWhitespaceAndBOM(t *testing.T) {
	input := byteOrderMark + "\r\n\r\n# Title\r\n\r\n\r\n\r\nBody line\n\n\n"
	got := NormalizeMarkdown(input)

	want := "# Title\n\nBody line"
	if got != want {
		t.Fatalf("normalize mismatch:\nwant %q\n got %q", want, got)
	}
}

func TestNormalizeMarkdownPreservesContentIndentation(t *testing.T) {
	input := "- item\n\t- nested item\n"
	got := NormalizeMarkdown(input)

	want := "- item\n\t- nested item"
	if got != want {
		t.Fatalf("indentation not preserved:\nwant %q\n got %q", want, got)
	}
}
