package markdown

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func newMarkdown() strategy.Strategy {
	return NewMarkdownStrategy(general.NewGeneralStrategy(nil, nil))
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
	s := markdownStrategy{GeneralStrategy: general.NewGeneralStrategy(nil, nil), maxTokens: 12, overlapTokens: 0, averageTokenLength: 1}
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

func TestMarkdownParseWithoutHeadings(t *testing.T) {
	parsed, err := newMarkdown().Parse([]byte("Just some plain text\nwith no headings at all."))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) != 1 || !strings.Contains(parsed.Sections[0].Body, "plain text") {
		t.Fatalf("expected a single whole-document section: %#v", parsed.Sections)
	}
}

func TestMarkdownParsePreambleBeforeFirstHeading(t *testing.T) {
	parsed, err := newMarkdown().Parse([]byte("Intro paragraph before any heading.\n\n# Guide\nBody."))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) < 2 || !strings.Contains(parsed.Sections[0].Body, "Intro paragraph") {
		t.Fatalf("expected a preamble section first: %#v", parsed.Sections)
	}
}

func TestMarkdownParseBlankIsNoSections(t *testing.T) {
	parsed, err := newMarkdown().Parse([]byte("   \n\n  "))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) != 0 {
		t.Fatalf("expected no sections for blank content: %#v", parsed.Sections)
	}
}

func TestMarkdownStrategyChunkSplitsOversizedFence(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Title\n\n```\n")
	for i := 0; i < 400; i++ {
		b.WriteString("this is a fairly long line of code inside a fenced block number\n")
	}
	b.WriteString("```\n")

	s := newMarkdown()
	parsed, err := s.Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := s.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected the oversized fence split into multiple chunks, got %d", len(chunks))
	}
}
