package config

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

func testSectionConfig(budget, maxDepth, maxChildren int) sectionConfig {
	return sectionConfig{
		budgetTokens:       budget,
		averageTokenLength: 4,
		maxDepth:           maxDepth,
		maxSectionChildren: maxChildren,
	}
}

// wideValue is long enough that a node holding it cannot fit a small budget.
func wideValue() string {
	return strings.Repeat("x", 400)
}

func paths(sections []strategy.Section) []string {
	out := make([]string, len(sections))
	for i, section := range sections {
		out[i] = strings.Join(section.Path, ">")
	}

	return out
}

// A subtree that already fits a chunk stays one section however deep it nests, which is the
// point of driving the descent by size rather than by depth.
func TestBuildSectionsKeepsDeepButSmallTreeWhole(t *testing.T) {
	root := node{Children: []node{{Key: "a", Children: []node{
		{Key: "b", Children: []node{
			{Key: "c", Children: []node{{Key: "d", Value: "1"}}},
		}},
	}}}}

	sections := buildSections(root, testSectionConfig(200, 4, 200))

	if len(sections) != 1 {
		t.Fatalf("want one section, got %d: %v", len(sections), paths(sections))
	}
	if len(sections[0].Path) != 0 {
		t.Fatalf("want the root path, got %v", sections[0].Path)
	}
}

func TestBuildSectionsOpensUpASubtreeTooLargeToFit(t *testing.T) {
	root := node{Children: []node{
		{Key: "database", Children: []node{{Key: "host", Value: wideValue()}}},
		{Key: "cache", Children: []node{{Key: "size", Value: wideValue()}}},
	}}

	sections := buildSections(root, testSectionConfig(20, 4, 200))

	got := paths(sections)
	want := []string{"database", "cache"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Settings that sit beside nested blocks keep the parent's title rather than being dropped.
func TestBuildSectionsEmitsLeavesBesideBranchesUnderTheParentPath(t *testing.T) {
	root := node{Children: []node{
		{Key: "version", Value: "2"},
		{Key: "database", Children: []node{{Key: "host", Value: wideValue()}}},
	}}

	sections := buildSections(root, testSectionConfig(20, 4, 200))

	got := paths(sections)
	if len(got) != 2 || got[0] != "" || got[1] != "database" {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(sections[0].Body, "version = 2") {
		t.Fatalf("leaf section lost its settings: %q", sections[0].Body)
	}
}

// Config formats nest without limit, so past the depth cap the remainder becomes body text
// instead of more titles. Nothing is lost, it just stops being a heading.
func TestBuildSectionsStopsTitlingAtTheDepthCap(t *testing.T) {
	root := node{Children: []node{{Key: "a", Children: []node{
		{Key: "b", Children: []node{
			{Key: "c", Children: []node{{Key: "d", Value: wideValue()}}},
		}},
	}}}}

	sections := buildSections(root, testSectionConfig(20, 2, 200))

	for _, section := range sections {
		if len(section.Path) > 2 {
			t.Fatalf("path deeper than the cap: %v", section.Path)
		}
	}
	if !strings.Contains(sections[len(sections)-1].Body, "d = ") {
		t.Fatalf("content past the cap was lost: %q", sections[len(sections)-1].Body)
	}
}

// A data dump would otherwise become one titled section per record.
func TestBuildSectionsEmitsAWideNodeWholeRatherThanExploding(t *testing.T) {
	children := make([]node, 0, 50)
	for i := 0; i < 50; i++ {
		children = append(children, node{Key: "item", Children: []node{{Key: "url", Value: wideValue()}}})
	}

	sections := buildSections(node{Children: children}, testSectionConfig(20, 4, 10))

	if len(sections) != 1 {
		t.Fatalf("want one section for a wide node, got %d", len(sections))
	}
}

func TestBuildSectionsSkipsEmptyBodies(t *testing.T) {
	if sections := buildSections(node{}, testSectionConfig(20, 4, 200)); len(sections) != 0 {
		t.Fatalf("want no sections, got %v", paths(sections))
	}
}

// The descent appends to a shared path slice, so each section must own its own copy.
func TestBuildSectionsGivesEachSectionItsOwnPath(t *testing.T) {
	root := node{Children: []node{
		{Key: "first", Children: []node{{Key: "v", Value: wideValue()}}},
		{Key: "second", Children: []node{{Key: "v", Value: wideValue()}}},
	}}

	sections := buildSections(root, testSectionConfig(20, 4, 200))

	if len(sections) != 2 {
		t.Fatalf("want two sections, got %d", len(sections))
	}
	if sections[0].Path[0] == sections[1].Path[0] {
		t.Fatalf("paths alias each other: %v and %v", sections[0].Path, sections[1].Path)
	}
}

// Anonymous members (array elements) still need a usable title.
func TestBuildSectionsNamesAnonymousChildrenByPosition(t *testing.T) {
	root := node{Children: []node{
		{Children: []node{{Key: "v", Value: wideValue()}}},
		{Children: []node{{Key: "v", Value: wideValue()}}},
	}}

	sections := buildSections(root, testSectionConfig(20, 4, 200))

	got := paths(sections)
	if len(got) != 2 || got[0] != "[0]" || got[1] != "[1]" {
		t.Fatalf("got %v", got)
	}
}
