package pipeline

import (
	"context"
	"testing"

	"github.com/davidbelicza/semantic-search/core/search"
	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestHitChunkIDs(t *testing.T) {
	ids := hitChunkIDs([]storage.VectorHit{{ChunkID: 3}, {ChunkID: 5}})
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 5 {
		t.Fatalf("chunk ids mismatch: %v", ids)
	}
}

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
	docs := groupDocuments(sampleResults(), search.SearchConfig{MaxDocuments: 2})

	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].DocumentID != 42 || docs[1].DocumentID != 7 {
		t.Fatalf("wrong documents kept: %#v", docs)
	}
}

func TestGroupDocumentsCapsChunksPerDocument(t *testing.T) {
	docs := groupDocuments(sampleResults(), search.SearchConfig{MaxChunks: 1})

	if len(docs[0].Chunks) != 1 || docs[0].Chunks[0].ChunkID != 1 {
		t.Fatalf("expected only the top chunk of document 42, got %#v", docs[0].Chunks)
	}
}

func TestFilterByRelevanceKeepsAtLeastMin(t *testing.T) {
	results := sampleResults() // distances 0.1, 0.2, 0.3, 0.4 => relevance 0.95, 0.90, 0.85, 0.80

	kept := filterByRelevance(results, 0.88)
	if len(kept) != 2 || kept[1].Score != 0.2 {
		t.Fatalf("expected the two matches at relevance >= 0.88, got %#v", kept)
	}

	if all := filterByRelevance(results, 0); len(all) != 4 {
		t.Fatalf("zero min relevance should keep all, got %d", len(all))
	}
}

// --- documentSearcher flow ---

type fakeVectorStore struct{ hits []storage.VectorHit }

func (f fakeVectorStore) Search(_ context.Context, _ []float32, limit int) ([]storage.VectorHit, error) {
	if limit < len(f.hits) {
		return f.hits[:limit], nil
	}
	return f.hits, nil
}

type fakeSearchStore struct {
	docByChunk map[int64]int64
	meta       map[int64]storage.ChunkMetadata
	paths      map[int64]string
	hydrated   []int64
}

func (f *fakeSearchStore) ChunkDocumentIDs(_ context.Context, ids []int64) ([]storage.ChunkDocument, error) {
	out := make([]storage.ChunkDocument, 0, len(ids))
	for _, id := range ids {
		out = append(out, storage.ChunkDocument{ChunkID: id, DocumentID: f.docByChunk[id]})
	}
	return out, nil
}

func (f *fakeSearchStore) ChunkMetadataByIDs(_ context.Context, ids []int64) ([]storage.ChunkMetadata, error) {
	f.hydrated = append(f.hydrated, ids...)
	out := make([]storage.ChunkMetadata, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.meta[id])
	}
	return out, nil
}

func (f *fakeSearchStore) DocumentsByIDs(_ context.Context, ids []int64) ([]storage.Document, error) {
	out := make([]storage.Document, 0, len(ids))
	for _, id := range ids {
		out = append(out, storage.Document{ID: id, AbsolutePath: f.paths[id]})
	}
	return out, nil
}

type fakeModel struct{}

func (fakeModel) Name() string                           { return "fake" }
func (fakeModel) Dimensions() int                        { return 3 }
func (fakeModel) BuildData(c storage.Chunk) string       { return c.Text }
func (fakeModel) BuildQuery(q, _ string) (string, error) { return q, nil }

type fakeClient struct{}

func (fakeClient) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return [][]float32{{1, 0, 0}}, nil
}

func TestDocumentSearcherGroupsAndHydratesSurvivorsOnly(t *testing.T) {
	store := &fakeSearchStore{
		docByChunk: map[int64]int64{1: 42, 2: 7, 3: 42, 4: 9},
		meta: map[int64]storage.ChunkMetadata{
			1: {ChunkID: 1, Title: "A", Text: "a"},
			2: {ChunkID: 2, Title: "B", Text: "b"},
			3: {ChunkID: 3, Title: "C", Text: "c"},
			4: {ChunkID: 4, Title: "D", Text: "d"},
		},
		paths: map[int64]string{42: "/x/first.md", 7: "/x/second.md", 9: "/x/third.md"},
	}
	vectors := fakeVectorStore{hits: []storage.VectorHit{
		{ChunkID: 1, Distance: 0.1},
		{ChunkID: 2, Distance: 0.2},
		{ChunkID: 3, Distance: 0.3},
		{ChunkID: 4, Distance: 0.4},
	}}

	searcher := NewDocumentSearcher(store, vectors, fakeModel{}, fakeClient{})
	docs, err := searcher.Search(context.Background(), search.SearchConfig{Query: "q", MaxChunks: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(docs) != 3 || docs[0].DocumentID != 42 || docs[1].DocumentID != 7 || docs[2].DocumentID != 9 {
		t.Fatalf("document set/order mismatch: %#v", docs)
	}
	if got := docs[0]; len(got.Chunks) != 1 || got.Chunks[0].ChunkID != 1 || got.Chunks[0].Text != "a" {
		t.Fatalf("first document not hydrated as expected: %#v", got)
	}
	if docs[0].FileName != "first.md" || docs[0].AbsolutePath != "/x/first.md" {
		t.Fatalf("path not filled: %#v", docs[0])
	}
	for _, id := range store.hydrated {
		if id == 3 {
			t.Fatalf("hydrated a dropped chunk (3); survivors were %v", store.hydrated)
		}
	}
}
