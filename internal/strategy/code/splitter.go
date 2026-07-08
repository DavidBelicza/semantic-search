package code

import (
	"strings"

	"github.com/alecthomas/chroma/v2"

	"github.com/davidbelicza/semantic-search/internal/strategy"
)

// CodeSplitter turns a lexed source file into structured sections. The family-specific part is
// only how a definition's boundaries are found (brace depth vs indentation); assembling marks
// into sections with a nesting path is shared (sectionsFromMarks).
type CodeSplitter interface {
	Split(source string, tokens []chroma.Token) []strategy.Section
}

// defMark is a detected definition: the source line it starts on (0-based) and its nesting
// path, e.g. ["class Invoice", "total()"]. The filename is prepended later, by the chunker's
// fallback-title mechanism, not here.
type defMark struct {
	line int
	path []string
}

// sectionsFromMarks slices the source into one section per definition (plus a leading section
// for any file header above the first definition). Each definition's start line is snapped
// backwards over its attached doc-comment and decorators so they embed with their definition.
func sectionsFromMarks(source string, marks []defMark) []strategy.Section {
	lines := strings.Split(source, "\n")
	if len(marks) == 0 {
		return flatSections(source)
	}

	starts := boundaryStarts(lines, marks)
	return assembleSections(lines, marks, starts)
}

// flatSections is the no-structure result: the whole file as a single untitled section. It is
// what the flat family (and any file with no detected definitions) produces.
func flatSections(source string) []strategy.Section {
	if strings.TrimSpace(source) == "" {
		return nil
	}

	return []strategy.Section{{Body: source}}
}

// boundaryStarts computes each definition's snapped start line, keeping starts strictly
// increasing so sections never overlap.
func boundaryStarts(lines []string, marks []defMark) []int {
	starts := make([]int, len(marks))
	floor := 0
	for i, mark := range marks {
		start := snapBackward(lines, mark.line, floor)
		starts[i] = start
		floor = start + 1
	}

	return starts
}

// snapBackward walks a definition's start line up over immediately preceding blank lines,
// comments, and decorators so they stay attached, without crossing the previous section.
func snapBackward(lines []string, line, floor int) int {
	start := line
	if start < floor {
		return floor
	}

	for start-1 >= floor && attachable(lines[start-1]) {
		start--
	}

	return start
}

// attachable reports whether a line belongs above the definition below it: blank lines,
// comments (across the languages we handle), and decorators/attributes.
func attachable(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}

	return hasCommentPrefix(trimmed) || strings.HasPrefix(trimmed, "@")
}

var commentPrefixes = []string{"//", "#", "/*", "*/", "*", "--", `"""`, "'''"}

func hasCommentPrefix(trimmed string) bool {
	for _, prefix := range commentPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	return false
}

// assembleSections builds the header section and one section per definition from the computed
// start lines.
func assembleSections(lines []string, marks []defMark, starts []int) []strategy.Section {
	sections := headerSection(lines, starts[0])

	for i, mark := range marks {
		end := len(lines)
		if i+1 < len(marks) {
			end = starts[i+1]
		}
		body := strings.Join(lines[starts[i]:end], "\n")
		sections = append(sections, strategy.Section{Path: mark.path, Body: body})
	}

	return sections
}

// headerSection returns the file header (imports, package/namespace, top-level statements)
// that sits above the first definition, or nothing when there is none.
func headerSection(lines []string, firstStart int) []strategy.Section {
	if firstStart <= 0 {
		return nil
	}

	header := strings.Join(lines[0:firstStart], "\n")
	if strings.TrimSpace(header) == "" {
		return nil
	}

	return []strategy.Section{{Body: header}}
}
