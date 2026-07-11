package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestMxbaiLargeModelMetadata(t *testing.T) {
	m := MxbaiLargeModel{}
	if m.Name() != "text-embedding-mxbai-embed-large-v1" {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if m.Dimensions() != 1024 {
		t.Fatalf("dimensions mismatch: %d", m.Dimensions())
	}
}

func TestMxbaiLargeModelBuildData(t *testing.T) {
	got := MxbaiLargeModel{}.BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestMxbaiLargeModelBuildQuery(t *testing.T) {
	got, err := MxbaiLargeModel{}.BuildQuery("how do I pay", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Represent this sentence for searching relevant passages: how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}

func TestMxbaiLargeModelBuildQueryRejectsTaskType(t *testing.T) {
	if _, err := (MxbaiLargeModel{}).BuildQuery("how do I pay", "classification"); err == nil {
		t.Fatal("expected an error for an unsupported task type")
	}
}
