package code

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"

	"github.com/davidbelicza/semantic-search/core/strategy"
)

// flatSplitter is the no-structure family: it emits the whole file as one section. Ruby and
// SQL use it in v1 — they are still normalized, chunked with overlap, and embedded, just
// without definition-level boundaries or titles. A structure-aware splitter can replace their
// registry entry later with no other change.
type flatSplitter struct{}

func (flatSplitter) Split(source string, _ []chroma.Token) []strategy.Section {
	return flatSections(source)
}

// splitters maps a file extension to the splitter family that handles it. Its keys are exactly
// the set of extensions the code strategy claims.
var splitters = map[string]CodeSplitter{
	".go":   braceSplitter{},
	".js":   braceSplitter{},
	".ts":   braceSplitter{},
	".jsx":  braceSplitter{},
	".tsx":  braceSplitter{},
	".java": braceSplitter{},
	".php":  braceSplitter{},
	".rs":   braceSplitter{},
	".c":    braceSplitter{},
	".h":    braceSplitter{},
	".cpp":  braceSplitter{},
	".hpp":  braceSplitter{},
	".cs":   braceSplitter{},
	".sh":   braceSplitter{},
	".py":   indentSplitter{},
	".rb":   flatSplitter{},
	".sql":  flatSplitter{},
}

func claimsExtension(path string) bool {
	_, ok := splitters[strings.ToLower(filepath.Ext(path))]
	return ok
}

func splitterFor(path string) CodeSplitter {
	return splitters[strings.ToLower(filepath.Ext(path))]
}
