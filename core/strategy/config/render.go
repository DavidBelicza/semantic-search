package config

import "strings"

const indentUnit = "  "

func renderSubtree(n node) string {
	var out strings.Builder
	writeChildren(&out, n.Children, 0)

	return strings.TrimRight(out.String(), "\n")
}

func writeChildren(out *strings.Builder, children []node, depth int) {
	for _, child := range children {
		writeNode(out, child, depth)
	}
}

func writeNode(out *strings.Builder, n node, depth int) {
	pad := strings.Repeat(indentUnit, depth)
	writeComment(out, n.Comment, pad)

	if n.isLeaf() {
		out.WriteString(pad + leafLine(n) + "\n")
		return
	}

	out.WriteString(pad + branchLabel(n) + "\n")
	writeChildren(out, n.Children, depth+1)
}

// writeComment renders the key's own comment above it. Comments are the most natural language
// a config file contains, so they are kept rather than dropped.
func writeComment(out *strings.Builder, comment string, pad string) {
	for _, line := range strings.Split(comment, "\n") {
		text := strings.TrimSpace(strings.TrimLeft(line, "#;"))
		if text != "" {
			out.WriteString(pad + "# " + text + "\n")
		}
	}
}

func leafLine(n node) string {
	value := valueFor(n)
	if n.Key == "" {
		return "- " + value
	}

	return n.Key + " = " + value
}

func branchLabel(n node) string {
	if n.Key == "" {
		return "-"
	}

	return n.Key + ":"
}

func valueFor(n node) string {
	if isSecretKey(n.Key) {
		return redactedValue
	}

	return n.Value
}
