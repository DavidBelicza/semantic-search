package config

import (
	"strconv"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

type sectionConfig struct {
	budgetTokens       int
	averageTokenLength int
	maxDepth           int
	maxSectionChildren int
}

// buildSections turns a config tree into sections titled by key path. The descent is driven by
// size, not depth: a subtree that fits a chunk becomes one section whatever its nesting, and
// only one too large is opened up. maxDepth is a backstop, since config nests without limit.
func buildSections(root node, config sectionConfig) []strategy.Section {
	return appendSections(nil, nil, root, config, 0)
}

func appendSections(dst []strategy.Section, path []string, n node, config sectionConfig, depth int) []strategy.Section {
	if isSectionLeaf(n, config, depth) {
		return appendSection(dst, path, n, config)
	}

	return appendChildSections(dst, path, n, config, depth)
}

func isSectionLeaf(n node, config sectionConfig, depth int) bool {
	return depth >= config.maxDepth ||
		len(branchChildren(n)) > config.maxSectionChildren ||
		fitsBudget(n, config)
}

func fitsBudget(n node, config sectionConfig) bool {
	rendered := renderSubtree(n)

	return textproc.EstimateTokenCount(rendered, config.averageTokenLength) <= config.budgetTokens
}

func appendSection(dst []strategy.Section, path []string, n node, config sectionConfig) []strategy.Section {
	body := renderSubtree(n)
	if body == "" {
		return dst
	}

	return append(dst, strategy.Section{Path: copyPath(path), Body: body})
}

func appendChildSections(dst []strategy.Section, path []string, n node, config sectionConfig, depth int) []strategy.Section {
	dst = appendLeafSection(dst, path, n, config)

	for i, child := range branchChildren(n) {
		dst = appendSections(dst, childPath(path, child, i), child, config, depth+1)
	}

	return dst
}

// appendLeafSection keeps settings that sit alongside nested blocks under the parent's title.
func appendLeafSection(dst []strategy.Section, path []string, n node, config sectionConfig) []strategy.Section {
	leaves := leafChildren(n)
	if len(leaves) == 0 {
		return dst
	}

	return appendSection(dst, path, node{Children: leaves}, config)
}

func childPath(path []string, child node, index int) []string {
	key := child.Key
	if key == "" {
		key = "[" + strconv.Itoa(index) + "]"
	}

	return append(copyPath(path), key)
}

func copyPath(path []string) []string {
	out := make([]string, len(path))
	copy(out, path)

	return out
}
