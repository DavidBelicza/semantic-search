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
	got, err := Qwen3SmallModel{}.BuildQuery("how do I pay", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Instruct: Search for this\nQuery: how do I pay"
	if got != want {
		t.Fatalf("build query mismatch: %q", got)
	}
}

func TestQwen3SmallModelBuildQueryWithTaskType(t *testing.T) {
	got, err := Qwen3SmallModel{}.BuildQuery("how do I pay", "Retrieve code that matches the query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Instruct: Retrieve code that matches the query\nQuery: how do I pay"
	if got != want {
		t.Fatalf("build query mismatch: %q", got)
	}
}
