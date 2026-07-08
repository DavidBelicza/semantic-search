package code

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// The splitters classify tokens by Chroma's token *category*, never by keyword spelling. A
// definition is a NameFunction/NameClass token that a declaration keyword introduced, so
// modifiers (public, static, readonly, async, ...) and language differences fall out for free.

func isKeyword(tok chroma.Token) bool {
	return tok.Type.Category() == chroma.Keyword
}

func isFuncName(tok chroma.Token) bool {
	return tok.Type == chroma.NameFunction
}

func isClassName(tok chroma.Token) bool {
	return tok.Type == chroma.NameClass
}

// isName reports a plain identifier (variable, attribute, call target). Seeing one ends a
// declaration prefix, so an in-body call like $this->total() is never mistaken for a def.
func isName(tok chroma.Token) bool {
	return tok.Type.Category() == chroma.Name && !isFuncName(tok) && !isClassName(tok)
}

func isOpenBrace(tok chroma.Token) bool {
	return tok.Type == chroma.Punctuation && strings.Contains(tok.Value, "{")
}

func isCloseBrace(tok chroma.Token) bool {
	return tok.Type == chroma.Punctuation && strings.Contains(tok.Value, "}")
}

func newlineCount(tok chroma.Token) int {
	return strings.Count(tok.Value, "\n")
}

func classTitle(name string) string {
	return "class " + name
}

func funcTitle(name string) string {
	return name + "()"
}
