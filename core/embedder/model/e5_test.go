package model

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestE5LargeModelMetadata(t *testing.T) {
	m := E5LargeModel{}
	if m.Name() != "text-embedding-multilingual-e5-large" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 1024 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestE5LargeModelBuildData(t *testing.T) {
	got := E5LargeModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "passage: pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
	if strings.Contains(got, "Payments") {
		t.Fatalf("build data must not include the title: %q", got)
	}
}

func TestE5LargeModelBuildQuery(t *testing.T) {
	got := E5LargeModel{}.BuildQuery("how do I pay")
	if got != "query: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}
