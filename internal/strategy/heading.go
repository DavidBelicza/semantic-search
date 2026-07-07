package strategy

// headingEntry is one level on the heading stack while a sectionizer assembles a document's
// sections. Both the Markdown and PDF sectionizers use it to track the current heading path.
type headingEntry struct {
	level int
	text  string
}

// pushHeading places a heading at its level, popping any deeper-or-equal headings so the
// stack always describes the current path from the top.
func pushHeading(stack []headingEntry, level int, text string) []headingEntry {
	for len(stack) > 0 && stack[len(stack)-1].level >= level {
		stack = stack[:len(stack)-1]
	}

	return append(stack, headingEntry{level: level, text: text})
}

// pathOf renders the heading stack as a path of heading texts.
func pathOf(stack []headingEntry) []string {
	path := make([]string, 0, len(stack))
	for _, entry := range stack {
		path = append(path, entry.text)
	}

	return path
}
