package semanticsearch

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestBuildSearchResultsResolvesInHitOrder(t *testing.T) {
	hits := []storage.VectorHit{
		{ChunkID: 7, Distance: 0.5},
		{ChunkID: 9, Distance: 0.8},
	}
	metadata := []storage.ChunkMetadata{
		{ChunkID: 9, DocumentID: 2, Title: "Refunds", Text: "refund the payment"},
		{ChunkID: 7, DocumentID: 42, Title: "Payments", Text: "pay the invoice"},
	}

	results := buildSearchResults(hits, metadata)

	if len(results) != 2 {
		t.Fatalf("result count mismatch: %d", len(results))
	}
	if got := results[0]; got.ChunkID != 7 || got.DocumentID != 42 || got.Title != "Payments" || got.Text != "pay the invoice" || got.Score != 0.5 {
		t.Fatalf("first result mismatch: %#v", got)
	}
	if results[1].ChunkID != 9 {
		t.Fatalf("hit order not preserved: %#v", results)
	}
}

func TestBuildSearchResultsSkipsMissingMetadata(t *testing.T) {
	hits := []storage.VectorHit{{ChunkID: 1, Distance: 0.1}}
	if results := buildSearchResults(hits, nil); len(results) != 0 {
		t.Fatalf("expected no results when metadata is missing, got %d", len(results))
	}
}

func TestHitChunkIDs(t *testing.T) {
	ids := hitChunkIDs([]storage.VectorHit{{ChunkID: 3}, {ChunkID: 5}})
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 5 {
		t.Fatalf("chunk ids mismatch: %v", ids)
	}
}
