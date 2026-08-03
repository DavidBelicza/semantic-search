package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestTitledTextPrefixesTheHeadingPath(t *testing.T) {
	got := titledText(storage.Chunk{Title: "Guide > Payments", Text: "pay the invoice"})
	if got != "Guide > Payments\npay the invoice" {
		t.Fatalf("expected the title prefixed, got %q", got)
	}
}

func TestTitledTextOmitsAnEmptyTitle(t *testing.T) {
	for _, title := range []string{"", "   "} {
		if got := titledText(storage.Chunk{Title: title, Text: "body"}); got != "body" {
			t.Fatalf("title %q: expected the body alone, got %q", title, got)
		}
	}
}
