package chunker

import (
	"context"
	"strings"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
)

func storageDocument(path string) storage.Document {
	return storage.Document{AbsolutePath: path}
}

func TestMarkdownChunkerSplitsSectionsWithHeadingPath(t *testing.T) {
	input := "# Guide\n## Payments\nPay the invoice.\n## Refunds\nRefund the payment."

	chunks, err := NewMarkdownChunker(0, 0).Chunk(context.Background(), Input{Text: input})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

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

func TestMarkdownChunkerUsesNoteNameAsTitleWhenNoHeading(t *testing.T) {
	input := "Marco Pierre White"

	chunks, err := NewMarkdownChunker(0, 0).Chunk(context.Background(), Input{
		Document: storageDocument("/notes/Marco Pierre White.md"),
		Text:     input,
	})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

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

func TestMarkdownChunkerTreatsHashTagsAsText(t *testing.T) {
	input := "#todo #urgent\n\nBuy milk and eggs."

	chunks, err := NewMarkdownChunker(0, 0).Chunk(context.Background(), Input{Text: input})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

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

func TestMarkdownChunkerIsDeterministic(t *testing.T) {
	input := "# A\nsome text here.\n## B\nmore text.\n\nanother paragraph."

	first, err := NewMarkdownChunker(0, 50).Chunk(context.Background(), Input{Text: input})
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	second, err := NewMarkdownChunker(0, 50).Chunk(context.Background(), Input{Text: input})
	if err != nil {
		t.Fatalf("second chunk: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("chunk count differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ContentHash != second[i].ContentHash {
			t.Fatalf("chunk %d content hash differs", i)
		}
	}
}

func TestMarkdownChunkerSplitsOversizedSection(t *testing.T) {
	chunker := MarkdownChunker{MaxTokens: 12, OverlapTokens: 0, AverageTokenLength: 1}
	input := "## S\n" + strings.Repeat("word ", 40)

	chunks, err := chunker.Chunk(context.Background(), Input{Text: input})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for oversized content, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Title != "S" {
			t.Fatalf("chunk missing heading title: %q", chunk.Title)
		}
	}
}
