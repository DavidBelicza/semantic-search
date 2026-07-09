package code

import (
	"strings"

	"github.com/alecthomas/chroma/v2"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

// indentSplitter handles indentation-delimited languages (Python). Definitions are found from
// tokens; their nesting is read from the source's leading indentation, since that is what
// actually encodes structure in these languages.
type indentSplitter struct{}

// anchor is a definition detected in the token stream, before its nesting path is resolved
// from indentation.
type anchor struct {
	line    int
	title   string
	isClass bool
}

func (indentSplitter) Split(source string, tokens []chroma.Token) []strategy.Section {
	anchors := indentAnchors(tokens)
	marks := resolveIndentPaths(anchors, strings.Split(source, "\n"))

	return sectionsFromMarks(source, marks)
}

// indentAnchors records def/class definitions from the token stream, using the same
// declaration-keyword gate as the brace family.
func indentAnchors(tokens []chroma.Token) []anchor {
	scanner := &indentScanner{}
	for _, tok := range tokens {
		scanner.consume(tok)
	}

	return scanner.anchors
}

type indentScanner struct {
	anchors []anchor
	inDecl  bool
	line    int
}

func (s *indentScanner) consume(tok chroma.Token) {
	s.classify(tok)
	s.line += newlineCount(tok)
}

func (s *indentScanner) classify(tok chroma.Token) {
	switch {
	case isKeyword(tok):
		s.inDecl = true
	case isClassName(tok) && s.inDecl:
		s.anchors = append(s.anchors, anchor{line: s.line, title: classTitle(tok.Value), isClass: true})
		s.inDecl = false
	case isFuncName(tok) && s.inDecl:
		s.anchors = append(s.anchors, anchor{line: s.line, title: funcTitle(tok.Value)})
		s.inDecl = false
	case isName(tok):
		s.inDecl = false
	}
}

// indentEntry is an open definition on the nesting stack, remembered with its indentation so
// deeper definitions nest under it and siblings pop it.
type indentEntry struct {
	indent int
	title  string
}

// resolveIndentPaths turns anchors into marks by tracking indentation: an anchor nests under
// every currently open definition with a strictly smaller indent.
func resolveIndentPaths(anchors []anchor, lines []string) []defMark {
	var stack []indentEntry
	marks := make([]defMark, 0, len(anchors))

	for _, a := range anchors {
		if a.line >= len(lines) {
			continue
		}
		indent := leadingIndent(lines[a.line])
		stack = popToIndent(stack, indent)
		marks = append(marks, defMark{line: a.line, path: titlesWith(stack, a.title)})
		stack = append(stack, indentEntry{indent: indent, title: a.title})
	}

	return marks
}

// popToIndent removes stack entries at or deeper than indent, leaving only the enclosing
// (strictly shallower) definitions.
func popToIndent(stack []indentEntry, indent int) []indentEntry {
	for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
		stack = stack[:len(stack)-1]
	}

	return stack
}

func titlesWith(stack []indentEntry, title string) []string {
	path := make([]string, 0, len(stack)+1)
	for _, entry := range stack {
		path = append(path, entry.title)
	}

	return append(path, title)
}

func leadingIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
