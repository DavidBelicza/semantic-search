package semanticsearch

import (
	"context"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/storage/sqlitevec"
)

type fakeMetadataStore struct {
	metadata []storage.ChunkMetadata
}

func (f fakeMetadataStore) ChunkMetadataByIDs(_ context.Context, _ []int64) ([]storage.ChunkMetadata, error) {
	return f.metadata, nil
}

type fakeVectorSearch struct {
	hits     []sqlitevec.VectorHit
	gotLimit int
}

func (f *fakeVectorSearch) Search(_ context.Context, _ []float32, limit int) ([]sqlitevec.VectorHit, error) {
	f.gotLimit = limit
	return f.hits, nil
}

type fakeQueryEmbedder struct{}

func (fakeQueryEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return [][]float32{{0.1, 0.2}}, nil
}

func TestSearchResolvesHitsToMetadataInOrder(t *testing.T) {
	metaStore := fakeMetadataStore{metadata: []storage.ChunkMetadata{
		{ChunkID: 7, DocumentID: 42, Title: "Payments", Text: "pay the invoice"},
	}}
	vectorStore := &fakeVectorSearch{hits: []sqlitevec.VectorHit{{ChunkID: 7, Distance: 0.5}}}

	results, err := search(context.Background(), metaStore, vectorStore, fakeQueryEmbedder{}, "invoice", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count mismatch: %d", len(results))
	}
	got := results[0]
	if got.DocumentID != 42 || got.ChunkID != 7 || got.Title != "Payments" || got.Text != "pay the invoice" || got.Score != 0.5 {
		t.Fatalf("result mismatch: %#v", got)
	}
	if vectorStore.gotLimit != 5 {
		t.Fatalf("limit not passed through: %d", vectorStore.gotLimit)
	}
}

func TestSearchRejectsBadInput(t *testing.T) {
	if _, err := Search(context.Background(), "x.db", "query", 0); err == nil {
		t.Fatal("expected error for non-positive limit")
	}
	if _, err := Search(context.Background(), "x.db", "   ", 5); err == nil {
		t.Fatal("expected error for blank query")
	}
}
