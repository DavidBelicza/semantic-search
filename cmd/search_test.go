package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"semantic-search/internal/storage/lancedb"
	storage "semantic-search/internal/storage/sqlite"
)

func TestNewSearchCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	searchCmd := NewSearchCommand(&out, &fakeDocumentStore{}, &fakeVectorStore{})
	searchCmd.SetArgs([]string{"--help"})

	if err := searchCmd.Execute(); err != nil {
		t.Fatalf("execute search help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "search [limit] [query]") {
		t.Fatalf("help output does not contain search usage: %q", help)
	}
}

func TestNewSearchCommandRequiresLimitAndQuery(t *testing.T) {
	var out bytes.Buffer
	searchCmd := NewSearchCommand(&out, &fakeDocumentStore{}, &fakeVectorStore{})
	searchCmd.SetArgs([]string{"how is payment configured"})

	if err := searchCmd.Execute(); err == nil {
		t.Fatal("expected error for missing limit or query")
	}
}

func TestNewSearchCommandReturnsDocumentChunkAndText(t *testing.T) {
	var out bytes.Buffer
	store := &searchMetadataStub{
		metadata: []storage.ChunkMetadata{
			{ChunkID: 7, DocumentID: 42, Title: "Payments > Providers", Text: "payment provider configuration"},
		},
	}
	vectorStore := &searchVectorStub{
		hits: []lancedb.VectorHit{{ChunkID: 7, Distance: 0.1}},
	}

	searchCmd := NewSearchCommandWithEmbedder(&out, store, vectorStore, stubQueryEmbedder{vector: []float32{0.1, 0.2}})
	searchCmd.SetArgs([]string{"5", "how is payment configured"})

	if err := searchCmd.Execute(); err != nil {
		t.Fatalf("execute search: %v", err)
	}

	output := out.String()
	for _, want := range []string{"document_id=42", "chunk_id=7", "score=0.1000", "[Payments > Providers]", "payment provider configuration"} {
		if !strings.Contains(output, want) {
			t.Fatalf("search output missing %q: %q", want, output)
		}
	}
	if vectorStore.gotLimit != 5 {
		t.Fatalf("expected limit 5 passed to vector store, got %d", vectorStore.gotLimit)
	}
}

func TestNewSearchCommandRejectsNonNumericLimit(t *testing.T) {
	var out bytes.Buffer
	searchCmd := NewSearchCommandWithEmbedder(&out, &searchMetadataStub{}, &searchVectorStub{}, stubQueryEmbedder{vector: []float32{0.1}})
	searchCmd.SetArgs([]string{"abc", "query"})

	if err := searchCmd.Execute(); err == nil {
		t.Fatal("expected error for non-numeric limit")
	}
}

type searchMetadataStub struct {
	metadata []storage.ChunkMetadata
}

func (s *searchMetadataStub) ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error) {
	return s.metadata, nil
}

type searchVectorStub struct {
	hits     []lancedb.VectorHit
	gotLimit int
}

func (s *searchVectorStub) Search(ctx context.Context, query []float32, limit int) ([]lancedb.VectorHit, error) {
	s.gotLimit = limit
	return s.hits, nil
}

type stubQueryEmbedder struct {
	vector []float32
}

func (e stubQueryEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return [][]float32{e.vector}, nil
}
