package model

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
)

func TestGeneralModelMetadata(t *testing.T) {
	m := NewGeneralModel("text-embedding-custom", 512)
	if m.Name() != "text-embedding-custom" || m.Dimensions() != 512 {
		t.Fatalf("general model not configured: name=%q dims=%d", m.Name(), m.Dimensions())
	}
}

func TestGeneralModelBuildDataOmitsTitle(t *testing.T) {
	got := NewGeneralModel("m", 1).BuildData(storage.Chunk{Title: "Payments", Text: "pay the invoice"})
	if got != "pay the invoice" {
		t.Fatalf("build data mismatch: %q", got)
	}
}

func TestGeneralModelBuildQuery(t *testing.T) {
	got, err := NewGeneralModel("m", 1).BuildQuery("how do I pay", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "how do I pay" {
		t.Fatalf("build query mismatch: %q", got)
	}
}

func TestGeneralModelBuildQueryRejectsTaskType(t *testing.T) {
	if _, err := NewGeneralModel("m", 1).BuildQuery("how do I pay", "classification"); err == nil {
		t.Fatal("expected an error for an unsupported task type")
	}
}
