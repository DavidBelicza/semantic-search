package config

import "strings"

// parseINI serves both .ini and .properties: .properties is .ini without section headers, and
// in both a dotted key names a path, so "[db.primary]" and "db.primary.host = x" agree.
func parseINI(source string) (node, error) {
	parser := iniParser{}
	for _, line := range strings.Split(source, "\n") {
		parser.consume(line)
	}

	return parser.root, nil
}

type iniParser struct {
	root    node
	section []string
	comment string
}

func (p *iniParser) consume(raw string) {
	line := strings.TrimSpace(raw)
	if line == "" {
		p.comment = ""
		return
	}
	if isCommentLine(line) {
		p.comment = appendComment(p.comment, line)
		return
	}
	if isSectionHeader(line) {
		p.section = splitKeyPath(strings.Trim(line, "[]"))
		p.comment = ""
		return
	}

	p.consumeEntry(line)
}

func (p *iniParser) consumeEntry(line string) {
	key, value, ok := splitEntry(line)
	if !ok {
		return
	}

	path := append(copyPath(p.section), splitKeyPath(key)...)
	leaf := node{Key: path[len(path)-1], Value: value, Comment: p.comment}

	p.root = insertPath(p.root, path[:len(path)-1], leaf)
	p.comment = ""
}

func isCommentLine(line string) bool {
	return strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "!")
}

func isSectionHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func appendComment(existing, line string) string {
	if existing == "" {
		return line
	}

	return existing + "\n" + line
}

func splitEntry(line string) (string, string, bool) {
	index := strings.IndexAny(line, "=:")
	if index < 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:index])
	value := strings.TrimSpace(line[index+1:])

	return key, value, key != ""
}

func splitKeyPath(key string) []string {
	parts := make([]string, 0, strings.Count(key, ".")+1)
	for _, part := range strings.Split(key, ".") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	if len(parts) == 0 {
		return []string{strings.TrimSpace(key)}
	}

	return parts
}
