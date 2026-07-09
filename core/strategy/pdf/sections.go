package pdf

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/internal/textproc"
)

const (
	// headingSizeRatio is how much larger than the body font a line must be to count as a
	// heading.
	headingSizeRatio = 1.15
	// fontSizeRounding groups nearly equal font sizes so minor rendering differences do not
	// fragment size classes.
	fontSizeRounding = 2
)

var (
	hyphenatedLineBreak    = regexp.MustCompile(`(\p{L})-\n(\p{Ll})`)
	headerFooterMinRepeats = 2
)

// textLine is a run of text grouped onto one visual line, with the font size and position
// used to infer headings and reading order.
type textLine struct {
	page     int
	top      float64
	left     float64
	fontSize float64
	text     string
}

// buildSectionsFromRuns turns positioned PDF text runs into sections: it drops repeated page
// headers/footers, groups runs into reading-ordered lines, detects the body font size, and
// splits the lines into sections at font-size-based headings.
func buildSectionsFromRuns(runs []TextRun) []strategy.Section {
	lines := groupRunsIntoLines(runs)
	lines = stripRepeatedHeadersAndFooters(lines)
	sortLinesIntoReadingOrder(lines)

	baseline := detectBodyFontSize(lines)
	return assembleSections(lines, baseline)
}

// groupRunsIntoLines merges runs that share a page and vertical position into single lines,
// ordering each line's text left to right.
func groupRunsIntoLines(runs []TextRun) []textLine {
	groups := map[string][]TextRun{}
	var order []string
	for _, run := range runs {
		if strings.TrimSpace(run.Text) == "" {
			continue
		}
		key := lineKey(run)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], run)
	}

	lines := make([]textLine, 0, len(order))
	for _, key := range order {
		lines = append(lines, lineFromRuns(groups[key]))
	}

	return lines
}

func lineKey(run TextRun) string {
	return strconv.Itoa(run.Page) + ":" + strconv.FormatFloat(math.Round(run.Y), 'f', 0, 64)
}

func lineFromRuns(runs []TextRun) textLine {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].X < runs[j].X })

	texts := make([]string, 0, len(runs))
	maxSize := 0.0
	for _, run := range runs {
		texts = append(texts, run.Text)
		if run.FontSize > maxSize {
			maxSize = run.FontSize
		}
	}

	return textLine{
		page:     runs[0].Page,
		top:      runs[0].Y,
		left:     runs[0].X,
		fontSize: maxSize,
		text:     strings.TrimSpace(strings.Join(textproc.NonEmptyTrimmed(texts), " ")),
	}
}

// stripRepeatedHeadersAndFooters removes lines whose text recurs at the same vertical
// position across multiple pages — running headers, footers, and page numbers.
func stripRepeatedHeadersAndFooters(lines []textLine) []textLine {
	if pageCount(lines) < 2 {
		return lines
	}

	repeats := map[string]int{}
	for _, line := range lines {
		repeats[headerFooterKey(line)]++
	}

	kept := make([]textLine, 0, len(lines))
	for _, line := range lines {
		if repeats[headerFooterKey(line)] >= headerFooterMinRepeats {
			continue
		}
		kept = append(kept, line)
	}

	return kept
}

func headerFooterKey(line textLine) string {
	return strconv.FormatFloat(math.Round(line.top), 'f', 0, 64) + "|" + strings.ToLower(line.text)
}

func pageCount(lines []textLine) int {
	pages := map[int]bool{}
	for _, line := range lines {
		pages[line.page] = true
	}

	return len(pages)
}

// sortLinesIntoReadingOrder orders lines top to bottom, left to right, page by page. Higher
// Top is higher on the page, so within a page lines sort by descending Top.
func sortLinesIntoReadingOrder(lines []textLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].page != lines[j].page {
			return lines[i].page < lines[j].page
		}
		if lines[i].top != lines[j].top {
			return lines[i].top > lines[j].top
		}
		return lines[i].left < lines[j].left
	})
}

// detectBodyFontSize returns the font size most of the text is set in (weighted by character
// count) — the baseline that headings stand out from.
func detectBodyFontSize(lines []textLine) float64 {
	weight := map[float64]int{}
	for _, line := range lines {
		weight[roundFontSize(line.fontSize)] += utf8.RuneCountInString(line.text)
	}

	best := 0.0
	bestWeight := -1
	for size, w := range weight {
		if w > bestWeight {
			bestWeight = w
			best = size
		}
	}

	return best
}

// assembleSections walks the ordered lines, opening a new section at each heading and
// accumulating the lines between headings as that section's body.
func assembleSections(lines []textLine, baseline float64) []strategy.Section {
	levels := headingLevelsBySize(lines, baseline)

	var sections []strategy.Section
	var stack []textproc.HeadingEntry
	var body []string

	flush := func() {
		joined := joinHyphenatedLineBreaks(strings.TrimSpace(strings.Join(body, "\n")))
		if joined != "" {
			sections = append(sections, strategy.Section{Path: textproc.PathOf(stack), Body: joined})
		}
		body = nil
	}

	for _, line := range lines {
		level, isHeading := levels[roundFontSize(line.fontSize)]
		if !isHeading {
			body = append(body, line.text)
			continue
		}
		flush()
		stack = textproc.PushHeading(stack, level, line.text)
	}
	flush()

	return sections
}

// headingLevelsBySize maps each above-baseline font size to a heading level, largest size as
// level 1. Sizes at or below the baseline are body text and absent from the map.
func headingLevelsBySize(lines []textLine, baseline float64) map[float64]int {
	threshold := baseline * headingSizeRatio

	present := map[float64]bool{}
	for _, line := range lines {
		size := roundFontSize(line.fontSize)
		if size > baseline && line.fontSize >= threshold {
			present[size] = true
		}
	}

	ordered := make([]float64, 0, len(present))
	for size := range present {
		ordered = append(ordered, size)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(ordered)))

	levels := make(map[float64]int, len(ordered))
	for i, size := range ordered {
		levels[size] = i + 1
	}

	return levels
}

// joinHyphenatedLineBreaks rejoins words split across a line break by a hyphen.
func joinHyphenatedLineBreaks(text string) string {
	return hyphenatedLineBreak.ReplaceAllString(text, "$1$2")
}

func roundFontSize(size float64) float64 {
	return math.Round(size*fontSizeRounding) / fontSizeRounding
}
