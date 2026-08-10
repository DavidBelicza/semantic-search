package config

import (
	"testing"
)

func TestInsertPathReusesAnExistingBranch(t *testing.T) {
	root := insertPath(node{}, []string{"a", "b"}, node{Key: "x", Value: "1"})
	root = insertPath(root, []string{"a", "b"}, node{Key: "y", Value: "2"})

	if len(root.Children) != 1 {
		t.Fatalf("want one top branch, got %d", len(root.Children))
	}
	if got := renderSubtree(root); got != "a:\n  b:\n    x = 1\n    y = 2" {
		t.Fatalf("got: %q", got)
	}
}

func TestInsertPathDoesNotReuseALeafAsABranch(t *testing.T) {
	root := insertPath(node{}, nil, node{Key: "a", Value: "1"})
	root = insertPath(root, []string{"a"}, node{Key: "b", Value: "2"})

	if got := renderSubtree(root); got != "a = 1\na:\n  b = 2" {
		t.Fatalf("got: %q", got)
	}
}
