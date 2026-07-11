package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestGemmaModelMetadata(t *testing.T) {
	m := GemmaModel{}
	if m.Name() != "text-embedding-embeddinggemma-300m-qat" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 768 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestGemmaModelBuildData(t *testing.T) {
	got := GemmaModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "title: Payments | text: pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestGemmaModelBuildDataEmptyTitle(t *testing.T) {
	got := GemmaModel{}.BuildData(storage.Chunk{Title: "  ", Text: "pay the invoice"})
	if got != "title: none | text: pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestGemmaModelBuildQuery(t *testing.T) {
	got, err := GemmaModel{}.BuildQuery("how do I pay", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "task: search result | query: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}

func TestGemmaModelBuildQueryWithTaskType(t *testing.T) {
	got, err := GemmaModel{}.BuildQuery("how do I pay", "classification")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "task: classification | query: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}
