package config

import (
	"path/filepath"
	"strings"
)

type parseFunc func(source string) (node, error)

// parsers maps an extension to its parser, and its keys are exactly what the strategy claims.
// ".xhtml" belongs to the HTML strategy: filepath.Ext returns only the segment after the last
// dot, so ".xml" and ".xhtml" are distinct keys that cannot collide.
var parsers = map[string]parseFunc{
	".json":       parseJSON,
	".xml":        parseXML,
	".yaml":       parseYAML,
	".yml":        parseYAML,
	".ini":        parseINI,
	".properties": parseINI,
}

func claimsExtension(path string) bool {
	_, ok := parsers[strings.ToLower(filepath.Ext(path))]

	return ok
}

func parserFor(path string) parseFunc {
	return parsers[strings.ToLower(filepath.Ext(path))]
}
