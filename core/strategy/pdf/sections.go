package pdf

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

const (
	// headingSizeRatio is how much larger than the body font a line must be to count as a
	// heading.
	headingSizeRatio = 1.15
	// fontSizeRounding groups nearly equal font sizes so minor rendering differences do not
	// fragment size classes.
	fontSizeRounding = 2
	// lineToleranceRatio is how far apart two runs' tops may be, as a fraction of the font
	// size, while still counting as the same baseline.
	lineToleranceRatio = 0.35
	// minLineTolerance keeps the baseline tolerance usable when a run reports no font size.
	minLineTolerance = 1.0
	// averageGlyphWidthRatio estimates a glyph's width as a fraction of the font size, used to
	// guess where a run ends.
	averageGlyphWidthRatio = 0.5
	// wordGapRatio is the gap, as a fraction of the font size, that separates words rather
	// than letters.
	wordGapRatio = 0.25
	// overprintOverlapRatio is how far into a run a repeat of it may start, as a fraction of
	// its width, and still count as the same glyphs drawn twice rather than a genuine repeat.
	overprintOverlapRatio = 0.75
)

var (
	hyphenatedLineBreak    = regexp.MustCompile(`(\p{L})-\n(\p{Ll})`)
	repeatedSpaces         = regexp.MustCompile(` {2,}`)
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

// groupRunsIntoLines merges runs that share a page and baseline into single lines, ordering
// each line's text left to right. A glyph's reported top depends on its shape — an "i" sits
// higher than an "o" — so runs on one baseline are matched within a tolerance rather than by
// an exact position, which would split a line into one fragment per glyph height.
func groupRunsIntoLines(runs []TextRun) []textLine {
	var lines []textLine
	var current []TextRun

	for _, run := range sortedRuns(runs) {
		if len(current) > 0 && !sameLine(current[0], run) {
			lines = append(lines, lineFromRuns(current))
			current = nil
		}
		current = append(current, run)
	}
	if len(current) > 0 {
		lines = append(lines, lineFromRuns(current))
	}

	return lines
}

// sortedRuns drops blank runs and orders the rest into reading order: page by page, top to
// bottom, left to right. Higher Y is higher on the page.
func sortedRuns(runs []TextRun) []TextRun {
	ordered := make([]TextRun, 0, len(runs))
	for _, run := range runs {
		if strings.TrimSpace(run.Text) == "" {
			continue
		}
		ordered = append(ordered, run)
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Page != ordered[j].Page {
			return ordered[i].Page < ordered[j].Page
		}
		if math.Abs(ordered[i].Y-ordered[j].Y) > lineTolerance(ordered[i].FontSize) {
			return ordered[i].Y > ordered[j].Y
		}
		return ordered[i].X < ordered[j].X
	})

	return ordered
}

// sameLine reports whether a run sits on the same page and baseline as the line's first run.
func sameLine(first, run TextRun) bool {
	if first.Page != run.Page {
		return false
	}

	return math.Abs(run.Y-first.Y) <= lineTolerance(first.FontSize)
}

// lineTolerance is how far a run's top may sit from its line's top and still belong to it. It
// scales with the font size so it stays well below the line spacing at any size.
func lineTolerance(fontSize float64) float64 {
	tolerance := fontSize * lineToleranceRatio
	if tolerance < minLineTolerance {
		return minLineTolerance
	}

	return tolerance
}

func lineFromRuns(runs []TextRun) textLine {
	maxSize := 0.0
	for _, run := range runs {
		if run.FontSize > maxSize {
			maxSize = run.FontSize
		}
	}

	return textLine{
		page:     runs[0].Page,
		top:      runs[0].Y,
		left:     runs[0].X,
		fontSize: maxSize,
		text:     joinRunText(runs),
	}
}

// joinRunText concatenates a line's runs. PDFium reports the spaces a PDF actually encodes, so
// runs are joined directly rather than with a separator — inserting one would space out the
// individual glyphs of a document that emits per-character runs. A space is added only when
// neither side supplies one and the runs sit far enough apart to be separate words.
func joinRunText(runs []TextRun) string {
	var text strings.Builder
	var previous *TextRun

	for i := range runs {
		// An overprint contributes no text, but it still advances the position the next run's
		// gap is measured from, since it sits to the right of the run it repeats.
		if previous != nil && isOverprint(*previous, runs[i]) {
			previous = &runs[i]
			continue
		}
		if previous != nil && needsSeparator(*previous, runs[i]) {
			text.WriteString(" ")
		}
		text.WriteString(runs[i].Text)
		previous = &runs[i]
	}

	return strings.TrimSpace(repeatedSpaces.ReplaceAllString(text.String(), " "))
}

// isOverprint reports whether a run repeats the previous one at an overlapping position. Some
// PDFs simulate bold by drawing the same glyphs twice a fraction of a character apart, which
// would otherwise double every letter of the word.
func isOverprint(previous, next TextRun) bool {
	if next.Text != previous.Text {
		return false
	}

	return next.X-previous.X < runWidth(previous)*overprintOverlapRatio
}

func needsSeparator(previous, next TextRun) bool {
	if strings.HasSuffix(previous.Text, " ") || strings.HasPrefix(next.Text, " ") {
		return false
	}

	return next.X > estimatedRunEnd(previous)+previous.FontSize*wordGapRatio
}

// estimatedRunEnd approximates where a run's text ends horizontally. PDFium reports a run's
// start but not its width, so the width is estimated from its glyph count.
func estimatedRunEnd(run TextRun) float64 {
	return run.X + runWidth(run)
}

func runWidth(run TextRun) float64 {
	return float64(utf8.RuneCountInString(run.Text)) * run.FontSize * averageGlyphWidthRatio
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

	sections := general.NewSectionizer("\n")
	for _, line := range lines {
		level, isHeading := levels[roundFontSize(line.fontSize)]
		if !isHeading {
			sections.AddBody(line.text)
			continue
		}
		sections.AddHeading(level, line.text)
	}

	return joinHyphenatedBodies(sections.Sections())
}

// joinHyphenatedBodies repairs words split across a line break, which can only be done once a
// section's lines are joined.
func joinHyphenatedBodies(sections []strategy.Section) []strategy.Section {
	for i, section := range sections {
		sections[i].Body = joinHyphenatedLineBreaks(section.Body)
	}

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
