package parser

import (
	"context"
	"testing"
)

func TestMarkdownParserReturnsText(t *testing.T) {
	got, err := (MarkdownParser{}).Parse(context.Background(), "# Title\n\nBody")
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}

	if got != "# Title\n\nBody" {
		t.Fatalf("parsed text mismatch: %q", got)
	}
}
