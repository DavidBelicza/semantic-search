package storage

import "testing"

func TestReconcileChunksKeepsInsertsAndRemoves(t *testing.T) {
	existing := []Chunk{
		{ID: 1, DocumentID: 7, ContentHash: "a"},
		{ID: 2, DocumentID: 7, ContentHash: "b"},
	}
	incoming := []Chunk{
		{ContentHash: "a", Text: "kept"},
		{ContentHash: "c", Text: "new"},
	}

	plan := ReconcileChunks(existing, incoming)

	if len(plan.Keep) != 1 || plan.Keep[0].ID != 1 || plan.Keep[0].DocumentID != 7 || plan.Keep[0].Text != "kept" {
		t.Fatalf("keep mismatch: %+v", plan.Keep)
	}
	if len(plan.Insert) != 1 || plan.Insert[0].ContentHash != "c" {
		t.Fatalf("insert mismatch: %+v", plan.Insert)
	}
	if len(plan.RemoveIDs) != 1 || plan.RemoveIDs[0] != 2 {
		t.Fatalf("remove mismatch: %+v", plan.RemoveIDs)
	}
}

func TestReconcileChunksMatchesDuplicateHashesOnce(t *testing.T) {
	existing := []Chunk{
		{ID: 1, ContentHash: "dup"},
		{ID: 2, ContentHash: "dup"},
	}
	incoming := []Chunk{{ContentHash: "dup"}}

	plan := ReconcileChunks(existing, incoming)

	if len(plan.Keep) != 1 || plan.Keep[0].ID != 1 {
		t.Fatalf("expected the first duplicate kept, got %+v", plan.Keep)
	}
	if len(plan.RemoveIDs) != 1 || plan.RemoveIDs[0] != 2 {
		t.Fatalf("expected the second duplicate removed, got %+v", plan.RemoveIDs)
	}
}

func TestReconcileChunksIgnoresUnsavedExisting(t *testing.T) {
	existing := []Chunk{{ID: 0, ContentHash: "x"}}
	incoming := []Chunk{{ContentHash: "y"}}

	plan := ReconcileChunks(existing, incoming)

	if len(plan.RemoveIDs) != 0 {
		t.Fatalf("expected no removals for an unsaved chunk, got %+v", plan.RemoveIDs)
	}
	if len(plan.Insert) != 1 {
		t.Fatalf("expected the incoming chunk inserted, got %+v", plan.Insert)
	}
}
