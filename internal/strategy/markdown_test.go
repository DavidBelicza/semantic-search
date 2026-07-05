package strategy

import (
	"strings"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
)

func markdown() Strategy {
	return NewMarkdownStrategy(NewGeneralStrategy(nil))
}

func TestMarkdownStrategyClaimsMarkdownOnly(t *testing.T) {
	for _, path := range []string{"a.md", "a.markdown", "a.mdown", "A.MD"} {
		if !markdown().Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	if markdown().Claims("a.txt") {
		t.Fatal("should not claim a.txt")
	}
}

func TestMarkdownStrategyParseNormalizes(t *testing.T) {
	got, err := markdown().Parse([]byte(byteOrderMark + "\r\n\r\n# Title\r\n\r\n\r\n\r\nBody\n\n\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != "# Title\n\nBody" {
		t.Fatalf("normalize mismatch: %q", got)
	}
}

func TestMarkdownStrategyChunkSplitsSectionsWithHeadingPath(t *testing.T) {
	chunks, err := markdown().Chunk(storage.Document{}, "# Guide\n## Payments\nPay the invoice.\n## Refunds\nRefund the payment.")
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
	input := "# A\nsome text.\n## B\nmore text.\n\nanother paragraph."
	first, _ := markdown().Chunk(storage.Document{}, input)
	second, _ := markdown().Chunk(storage.Document{}, input)
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
	s := markdownStrategy{general: NewGeneralStrategy(nil), maxTokens: 12, overlapTokens: 0, averageTokenLength: 1}
	chunks, err := s.Chunk(storage.Document{}, "## S\n"+strings.Repeat("word ", 40))
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
