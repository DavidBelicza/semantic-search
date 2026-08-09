package config

import (
	"errors"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// parseYAML decodes into yaml.Node rather than a plain map so key order survives and comments
// come with it: a comment is often the only natural language a config file has. Each document
// of a multi-document stream becomes an anonymous branch.
func parseYAML(source string) (node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(source))

	var documents []node
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return yamlRoot(documents), nil
		}
		if err != nil {
			return node{}, err
		}

		documents = append(documents, yamlDocument(document))
	}
}

func yamlRoot(documents []node) node {
	if len(documents) == 1 {
		return documents[0]
	}

	return node{Children: documents}
}

func yamlDocument(document yaml.Node) node {
	if len(document.Content) == 0 {
		return node{}
	}

	return yamlValue("", document.Content[0])
}

func yamlValue(key string, value *yaml.Node) node {
	switch value.Kind {
	case yaml.MappingNode:
		return node{Key: key, Comment: yamlComment(value), Children: yamlPairs(value)}
	case yaml.SequenceNode:
		return node{Key: key, Comment: yamlComment(value), Children: yamlItems(value)}
	case yaml.AliasNode:
		return yamlAlias(key, value)
	default:
		return node{Key: key, Value: value.Value, Comment: yamlComment(value)}
	}
}

func yamlPairs(mapping *yaml.Node) []node {
	children := make([]node, 0, len(mapping.Content)/2)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		child := yamlValue(key.Value, mapping.Content[i+1])
		children = append(children, withKeyComment(child, key))
	}

	return children
}

func yamlItems(sequence *yaml.Node) []node {
	children := make([]node, 0, len(sequence.Content))
	for _, item := range sequence.Content {
		children = append(children, yamlValue("", item))
	}

	return children
}

func yamlAlias(key string, value *yaml.Node) node {
	if value.Alias == nil {
		return node{Key: key}
	}

	return yamlValue(key, value.Alias)
}

func withKeyComment(child node, key *yaml.Node) node {
	comment := yamlComment(key)
	if comment == "" {
		return child
	}

	child.Comment = comment

	return child
}

func yamlComment(value *yaml.Node) string {
	parts := make([]string, 0, 2)
	for _, comment := range []string{value.HeadComment, value.LineComment} {
		if strings.TrimSpace(comment) != "" {
			parts = append(parts, comment)
		}
	}

	return strings.Join(parts, "\n")
}
