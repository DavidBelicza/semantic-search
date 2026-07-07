package textproc

// HeadingEntry is one level on the heading stack while a sectionizer assembles a document's
// sections. It tracks the current heading path.
type HeadingEntry struct {
	Level int
	Text  string
}

// PushHeading places a heading at its level, popping any deeper-or-equal headings so the
// stack always describes the current path from the top.
func PushHeading(stack []HeadingEntry, level int, text string) []HeadingEntry {
	for len(stack) > 0 && stack[len(stack)-1].Level >= level {
		stack = stack[:len(stack)-1]
	}

	return append(stack, HeadingEntry{Level: level, Text: text})
}

// PathOf renders the heading stack as a path of heading texts.
func PathOf(stack []HeadingEntry) []string {
	path := make([]string, 0, len(stack))
	for _, entry := range stack {
		path = append(path, entry.Text)
	}

	return path
}
