package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestBGELargeModelMetadata(t *testing.T) {
	m := BGELargeModel{}
	if m.Name() != "text-embedding-bge-large-en-v1.5" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 1024 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestBGELargeModelBuildData(t *testing.T) {
	got := BGELargeModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestBGELargeModelBuildQuery(t *testing.T) {
	got := BGELargeModel{}.BuildQuery("how do I pay")
	if got != "Represent this sentence for searching relevant passages: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}
