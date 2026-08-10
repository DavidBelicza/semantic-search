package config

// node is the format-agnostic config tree every parser decodes into. A node with no children
// is a leaf carrying Value; one with children is a branch. Key is empty for anonymous members
// such as JSON array elements. Comment is the format's own comment, where it has one.
type node struct {
	Key      string
	Value    string
	Comment  string
	Children []node
}

func (n node) isLeaf() bool {
	return len(n.Children) == 0
}

func leafChildren(n node) []node {
	leaves := make([]node, 0, len(n.Children))
	for _, child := range n.Children {
		if child.isLeaf() {
			leaves = append(leaves, child)
		}
	}

	return leaves
}

func branchChildren(n node) []node {
	branches := make([]node, 0, len(n.Children))
	for _, child := range n.Children {
		if !child.isLeaf() {
			branches = append(branches, child)
		}
	}

	return branches
}

// insertPath places a leaf at a key path, creating branches on the way. It is how the
// line-based formats turn a dotted key into nesting.
func insertPath(root node, path []string, leaf node) node {
	if len(path) == 0 {
		root.Children = append(root.Children, leaf)
		return root
	}

	index := indexOfChild(root, path[0])
	if index < 0 {
		root.Children = append(root.Children, node{Key: path[0]})
		index = len(root.Children) - 1
	}

	root.Children[index] = insertPath(root.Children[index], path[1:], leaf)

	return root
}

func indexOfChild(parent node, key string) int {
	for i, child := range parent.Children {
		if child.Key == key && !child.isLeaf() {
			return i
		}
	}

	return -1
}
