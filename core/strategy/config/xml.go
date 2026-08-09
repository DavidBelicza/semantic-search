package config

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// parseXML makes elements branches and attributes leaves prefixed with "@". It walks the token
// stream with an explicit stack rather than unmarshalling, since indexed documents have no
// schema to bind to. The decoder is non-strict so an odd document still yields its text.
func parseXML(source string) (node, error) {
	decoder := xml.NewDecoder(strings.NewReader(source))
	decoder.Strict = false

	stack := []node{{}}
	comment := ""

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return stack[0], nil
		}
		if err != nil {
			return node{}, err
		}

		stack, comment = consumeXMLToken(stack, token, comment)
	}
}

func consumeXMLToken(stack []node, token xml.Token, comment string) ([]node, string) {
	switch typed := token.(type) {
	case xml.StartElement:
		return append(stack, startXMLElement(typed, comment)), ""
	case xml.CharData:
		return appendXMLText(stack, string(typed)), comment
	case xml.EndElement:
		return closeXMLElement(stack), comment
	case xml.Comment:
		return stack, strings.TrimSpace(string(typed))
	default:
		return stack, comment
	}
}

func startXMLElement(element xml.StartElement, comment string) node {
	return node{
		Key:      element.Name.Local,
		Comment:  comment,
		Children: attributeChildren(element.Attr),
	}
}

func attributeChildren(attrs []xml.Attr) []node {
	children := make([]node, 0, len(attrs))
	for _, attr := range attrs {
		children = append(children, node{Key: "@" + attr.Name.Local, Value: attr.Value})
	}

	return children
}

// An element that already has children keeps its text as an anonymous leaf, so mixed content
// is not lost to the branch/leaf distinction.
func appendXMLText(stack []node, text string) []node {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return stack
	}

	top := len(stack) - 1
	if stack[top].isLeaf() {
		stack[top].Value = joinText(stack[top].Value, trimmed)
		return stack
	}

	stack[top].Children = append(stack[top].Children, node{Value: trimmed})

	return stack
}

func closeXMLElement(stack []node) []node {
	if len(stack) < 2 {
		return stack
	}

	finished := stack[len(stack)-1]
	stack = stack[:len(stack)-1]
	parent := len(stack) - 1
	stack[parent].Children = append(stack[parent].Children, finished)

	return stack
}

func joinText(existing, addition string) string {
	if existing == "" {
		return addition
	}

	return existing + " " + addition
}
