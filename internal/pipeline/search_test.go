package pipeline

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/search"
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

func intPtr(v int) *int { return &v }

// ranked hits: doc 42 (0.1, 0.3), doc 7 (0.2), doc 9 (0.4). Best-first order.
func sampleResults() []search.SearchResult {
	return []search.SearchResult{
		{DocumentID: 42, ChunkID: 1, Score: 0.1},
		{DocumentID: 7, ChunkID: 2, Score: 0.2},
		{DocumentID: 42, ChunkID: 3, Score: 0.3},
		{DocumentID: 9, ChunkID: 4, Score: 0.4},
	}
}

func TestGroupDocumentsRanksByBestChunk(t *testing.T) {
	docs := groupDocuments(sampleResults(), search.SearchConfig{})

	if len(docs) != 3 {
		t.Fatalf("document count mismatch: %d", len(docs))
	}
	if docs[0].DocumentID != 42 || docs[0].Score != 0.1 || len(docs[0].Chunks) != 2 {
		t.Fatalf("first document mismatch: %#v", docs[0])
	}
	if docs[1].DocumentID != 7 || docs[2].DocumentID != 9 {
		t.Fatalf("document order not by best chunk: %#v", docs)
	}
}

func TestGroupDocumentsCapsDocuments(t *testing.T) {
	docs := groupDocuments(sampleResults(), search.SearchConfig{MaxDocuments: intPtr(2)})

	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].DocumentID != 42 || docs[1].DocumentID != 7 {
		t.Fatalf("wrong documents kept: %#v", docs)
	}
}

func TestGroupDocumentsCapsChunksPerDocument(t *testing.T) {
	docs := groupDocuments(sampleResults(), search.SearchConfig{MaxChunks: intPtr(1)})

	if len(docs[0].Chunks) != 1 || docs[0].Chunks[0].ChunkID != 1 {
		t.Fatalf("expected only the top chunk of document 42, got %#v", docs[0].Chunks)
	}
}
