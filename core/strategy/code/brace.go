package code

import (
	"github.com/alecthomas/chroma/v2"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

// braceSplitter handles brace-delimited languages (Go, JS/TS, Java, PHP, Rust, C/C++, C#,
// shell). Nesting is tracked by brace depth, counting only real Punctuation braces — braces
// inside strings and comments are other token types, so they never miscount.
type braceSplitter struct{}

// container is an enclosing class pushed onto the nesting stack, remembered with the brace
// depth at which its body opened so it can be popped when that body closes.
type container struct {
	title string
	depth int
}

func (braceSplitter) Split(source string, tokens []chroma.Token) []strategy.Section {
	return sectionsFromMarks(source, braceMarks(tokens))
}

// braceMarks walks the token stream and records every function/class definition with its
// enclosing-class path.
func braceMarks(tokens []chroma.Token) []defMark {
	scanner := &braceScanner{}
	for _, tok := range tokens {
		scanner.consume(tok)
	}

	return scanner.marks
}

// braceScanner is the mutable walk state. Keeping it a struct with one method per token role
// keeps braceMarks flat.
type braceScanner struct {
	marks        []defMark
	stack        []container
	depth        int
	inDecl       bool
	pendingClass string
	line         int
}

func (s *braceScanner) consume(tok chroma.Token) {
	s.classify(tok)
	s.line += newlineCount(tok)
}

func (s *braceScanner) classify(tok chroma.Token) {
	switch {
	case isKeyword(tok):
		s.inDecl = true
	case isClassName(tok) && s.inDecl:
		s.pendingClass = classTitle(tok.Value)
		s.inDecl = false
	case isFuncName(tok) && s.inDecl:
		s.marks = append(s.marks, defMark{line: s.line, path: funcPath(s.stack, tok.Value)})
		s.inDecl = false
	case isOpenBrace(tok):
		s.openBrace()
	case isCloseBrace(tok):
		s.closeBrace()
	case isName(tok):
		s.inDecl = false
	}
}

// openBrace descends one level and, if a class name is pending, pushes that class as the
// container for this new body.
func (s *braceScanner) openBrace() {
	s.depth++
	if s.pendingClass == "" {
		return
	}

	s.stack = append(s.stack, container{title: s.pendingClass, depth: s.depth})
	s.pendingClass = ""
}

// closeBrace ascends one level and pops any container whose body has now closed.
func (s *braceScanner) closeBrace() {
	s.depth--
	for len(s.stack) > 0 && s.stack[len(s.stack)-1].depth > s.depth {
		s.stack = s.stack[:len(s.stack)-1]
	}
}

// funcPath builds a function's nesting path: the enclosing class titles plus the function.
func funcPath(stack []container, name string) []string {
	path := make([]string, 0, len(stack)+1)
	for _, entry := range stack {
		path = append(path, entry.title)
	}

	return append(path, funcTitle(name))
}
