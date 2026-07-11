package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestQwen3SmallModelMetadata(t *testing.T) {
	m := Qwen3SmallModel{}
	if m.Name() != "text-embedding-qwen3-embedding-0.6b" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 1024 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestQwen3SmallModelBuildData(t *testing.T) {
	got := Qwen3SmallModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestQwen3SmallModelBuildQuery(t *testing.T) {
	got := Qwen3SmallModel{}.BuildQuery("how do I pay")
	want := "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: how do I pay"
	if got != want {
		t.Fatalf("build query mismatch: %q", got)
	}
}
