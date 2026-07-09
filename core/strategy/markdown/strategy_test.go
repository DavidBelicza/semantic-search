package markdown

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func newMarkdown() strategy.Strategy {
	return NewMarkdownStrategy(general.NewGeneralStrategy(nil))
}

func TestMarkdownStrategyClaimsMarkdownOnly(t *testing.T) {
	for _, path := range []string{"a.md", "a.markdown", "a.mdown", "A.MD"} {
		if !newMarkdown().Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	if newMarkdown().Claims("a.txt") {
		t.Fatal("should not claim a.txt")
	}
}

func TestMarkdownStrategyChunkSplitsSectionsWithHeadingPath(t *testing.T) {
	s := newMarkdown()
	parsed, err := s.Parse([]byte("# Guide\n## Payments\nPay the invoice.\n## Refunds\nRefund the payment."))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := s.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count mismatch: want 2, got %d", len(chunks))
	}
	if chunks[0].Title != "Guide > Payments" || chunks[1].Title != "Guide > Refunds" {
		t.Fatalf("heading paths mismatch: %q, %q", chunks[0].Title, chunks[1].Title)
	}
}

func TestMarkdownStrategyChunkIsDeterministic(t *testing.T) {
	s := newMarkdown()
	parsed, err := s.Parse([]byte("# A\nsome text.\n## B\nmore text.\n\nanother paragraph."))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	first, _ := s.Chunk(storage.Document{}, parsed)
	second, _ := s.Chunk(storage.Document{}, parsed)
	if len(first) != len(second) {
		t.Fatalf("chunk count differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ContentHash != second[i].ContentHash {
			t.Fatalf("chunk %d hash differs", i)
		}
	}
}

func TestMarkdownStrategyChunkSplitsOversizedSection(t *testing.T) {
	s := markdownStrategy{GeneralStrategy: general.NewGeneralStrategy(nil), maxTokens: 12, overlapTokens: 0, averageTokenLength: 1}
	parsed, err := s.Parse([]byte("## S\n" + strings.Repeat("word ", 40)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := s.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Title != "S" {
			t.Fatalf("chunk missing heading title: %q", chunk.Title)
		}
	}
}
