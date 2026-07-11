package model

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestNomicModelMetadata(t *testing.T) {
	m := NomicModel{}
	if m.Name() != "text-embedding-nomic-embed-text-v1.5" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 768 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestNomicModelBuildData(t *testing.T) {
	got := NomicModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "search_document: pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
	if strings.Contains(got, "Payments") {
		t.Fatalf("build data must not include the title: %q", got)
	}
}

func TestNomicModelBuildQuery(t *testing.T) {
	got := NomicModel{}.BuildQuery("how do I pay")
	if got != "search_query: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}
