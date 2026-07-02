package strategy

import (
	"context"
	"strings"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
)

func chunkMarkdown(t *testing.T, s markdownStrategy, doc storage.Document, text string) []storage.Chunk {
	t.Helper()
	chunks, err := s.Chunk(context.Background(), doc, text)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	return chunks
}

func TestMarkdownStrategyParseKeepsCleanTextUnchanged(t *testing.T) {
	got, err := NewMarkdownStrategy().Parse(context.Background(), "# Title\n\nBody")
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}

	if got != "# Title\n\nBody" {
		t.Fatalf("parsed text mismatch: %q", got)
	}
}

func TestMarkdownStrategyParseCleansWhitespaceAndBOM(t *testing.T) {
	input := byteOrderMark + "\r\n\r\n# Title\r\n\r\n\r\n\r\nBody line\n\n\n"
	got, err := NewMarkdownStrategy().Parse(context.Background(), input)
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}

	want := "# Title\n\nBody line"
	if got != want {
		t.Fatalf("normalize mismatch:\nwant %q\n got %q", want, got)
	}
}

func TestMarkdownStrategyParsePreservesContentIndentation(t *testing.T) {
	got, err := NewMarkdownStrategy().Parse(context.Background(), "- item\n\t- nested item\n")
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}

	want := "- item\n\t- nested item"
	if got != want {
		t.Fatalf("indentation not preserved:\nwant %q\n got %q", want, got)
	}
}

func TestMarkdownStrategySplitsSectionsWithHeadingPath(t *testing.T) {
	input := "# Guide\n## Payments\nPay the invoice.\n## Refunds\nRefund the payment."

	chunks := chunkMarkdown(t, newMarkdownStrategy(0, 0), storage.Document{}, input)

	if len(chunks) != 2 {
		t.Fatalf("chunk count mismatch: want 2, got %d (%#v)", len(chunks), chunks)
	}
	if chunks[0].Title != "Guide > Payments" || chunks[0].Text != "Pay the invoice." {
		t.Fatalf("first chunk mismatch: title=%q text=%q", chunks[0].Title, chunks[0].Text)
	}
	if chunks[1].Title != "Guide > Refunds" || chunks[1].Text != "Refund the payment." {
		t.Fatalf("second chunk mismatch: title=%q text=%q", chunks[1].Title, chunks[1].Text)
	}
	if chunks[0].ChunkIndex != 0 || chunks[1].ChunkIndex != 1 {
		t.Fatalf("chunk indexes mismatch: %d, %d", chunks[0].ChunkIndex, chunks[1].ChunkIndex)
	}
}

func TestMarkdownStrategyUsesNoteNameAsTitleWhenNoHeading(t *testing.T) {
	chunks := chunkMarkdown(t, newMarkdownStrategy(0, 0), storage.Document{AbsolutePath: "/notes/Marco Pierre White.md"}, "Marco Pierre White")

	if len(chunks) != 1 {
		t.Fatalf("chunk count mismatch: want 1, got %d", len(chunks))
	}
	if chunks[0].Title != "Marco Pierre White" {
		t.Fatalf("expected note name as title, got %q", chunks[0].Title)
	}
	if chunks[0].Text != "Marco Pierre White" {
		t.Fatalf("text mismatch: %q", chunks[0].Text)
	}
}

func TestMarkdownStrategyTreatsHashTagsAsText(t *testing.T) {
	chunks := chunkMarkdown(t, newMarkdownStrategy(0, 0), storage.Document{}, "#todo #urgent\n\nBuy milk and eggs.")

	if len(chunks) != 1 {
		t.Fatalf("chunk count mismatch: want 1, got %d (%#v)", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Text, "#todo #urgent") {
		t.Fatalf("tag text missing: %q", chunks[0].Text)
	}
	if chunks[0].Title != "" {
		t.Fatalf("tags were misread as a heading path: title=%q", chunks[0].Title)
	}
}

func TestMarkdownStrategyIsDeterministic(t *testing.T) {
	input := "# A\nsome text here.\n## B\nmore text.\n\nanother paragraph."

	first := chunkMarkdown(t, newMarkdownStrategy(0, 50), storage.Document{}, input)
	second := chunkMarkdown(t, newMarkdownStrategy(0, 50), storage.Document{}, input)

	if len(first) != len(second) {
		t.Fatalf("chunk count differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ContentHash != second[i].ContentHash {
			t.Fatalf("chunk %d content hash differs", i)
		}
	}
}

func TestMarkdownStrategySplitsOversizedSection(t *testing.T) {
	strategy := markdownStrategy{maxTokens: 12, overlapTokens: 0, averageTokenLength: 1}
	input := "## S\n" + strings.Repeat("word ", 40)

	chunks := chunkMarkdown(t, strategy, storage.Document{}, input)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized content, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Title != "S" {
			t.Fatalf("chunk missing heading title: %q", chunk.Title)
		}
	}
}
