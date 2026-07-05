package strategy

import "testing"

func TestPoolForReturnsClaimingStrategy(t *testing.T) {
	pool := NewPool(NewMarkdownStrategy(NewGeneralStrategy(nil)))

	if _, ok := pool.For("note.md"); !ok {
		t.Fatal("expected a strategy for note.md")
	}
	if _, ok := pool.For("note.txt"); ok {
		t.Fatal("expected no strategy for note.txt")
	}
}

func TestPoolForFallsThroughToGeneral(t *testing.T) {
	pool := NewPool(NewMarkdownStrategy(NewGeneralStrategy(nil)), NewGeneralStrategy(nil))

	if _, ok := pool.For("note.txt"); !ok {
		t.Fatal("general strategy should claim note.txt")
	}
}
