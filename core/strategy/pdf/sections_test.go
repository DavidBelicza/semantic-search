package pdf

import "testing"

// run builds a text run at a position, with a default 10pt font unless a size is given.
func run(text string, x, y float64, size ...float64) TextRun {
	fontSize := 10.0
	if len(size) > 0 {
		fontSize = size[0]
	}

	return TextRun{Text: text, X: x, Y: y, FontSize: fontSize}
}

func TestGroupRunsIntoLinesMergesGlyphTops(t *testing.T) {
	// A glyph's reported top depends on its shape, so one baseline reports several tops. All
	// of these belong to a single line.
	lines := groupRunsIntoLines([]TextRun{
		run("c", 10, 792.2), run("i", 15, 794.9), run("f", 20, 795.1), run("o", 25, 792.2),
	})

	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d: %+v", len(lines), lines)
	}
	if lines[0].text != "cifo" {
		t.Fatalf("expected the glyphs joined, got %q", lines[0].text)
	}
}

func TestGroupRunsIntoLinesSeparatesRealLines(t *testing.T) {
	lines := groupRunsIntoLines([]TextRun{
		run("first", 10, 700), run("second", 10, 680),
	})

	if len(lines) != 2 || lines[0].text != "first" || lines[1].text != "second" {
		t.Fatalf("expected two lines top to bottom, got %+v", lines)
	}
}

func TestGroupRunsIntoLinesSeparatesPages(t *testing.T) {
	lines := groupRunsIntoLines([]TextRun{
		{Text: "one", X: 10, Y: 700, FontSize: 10, Page: 0},
		{Text: "two", X: 10, Y: 700, FontSize: 10, Page: 1},
	})

	if len(lines) != 2 {
		t.Fatalf("runs on different pages must not merge, got %+v", lines)
	}
}

func TestGroupRunsIntoLinesDropsBlankRuns(t *testing.T) {
	lines := groupRunsIntoLines([]TextRun{run("  ", 10, 700), run("text", 20, 700)})

	if len(lines) != 1 || lines[0].text != "text" {
		t.Fatalf("expected the blank run dropped, got %+v", lines)
	}
}

func TestGroupRunsIntoLinesHandlesNoRuns(t *testing.T) {
	if lines := groupRunsIntoLines(nil); len(lines) != 0 {
		t.Fatalf("expected no lines, got %+v", lines)
	}
}

func TestJoinRunTextKeepsEncodedSpacesAndAddsWordGaps(t *testing.T) {
	// PDFium reports the spaces a PDF encodes, so adjacent runs concatenate directly.
	joined := joinRunText([]TextRun{run("Va", 10, 700), run("c", 20, 700), run("cine ", 25, 700)})
	if joined != "Vaccine" {
		t.Fatalf("expected adjacent glyphs concatenated, got %q", joined)
	}

	// A run starting well past the previous one is a separate word.
	gapped := joinRunText([]TextRun{run("left", 10, 700), run("right", 300, 700)})
	if gapped != "left right" {
		t.Fatalf("expected a space across a wide gap, got %q", gapped)
	}
}

func TestJoinRunTextDropsOverprintedGlyphs(t *testing.T) {
	// Some PDFs fake bold by drawing the same glyphs twice a fraction of a character apart.
	joined := joinRunText([]TextRun{
		run("TA", 308.3, 740, 9), run("TA", 312.9, 740, 9), run("J ", 319.6, 740, 9),
	})

	if joined != "TAJ" {
		t.Fatalf("expected the overprinted glyphs dropped, got %q", joined)
	}
}

func TestJoinRunTextKeepsGenuineRepeats(t *testing.T) {
	// The same text at a full character's distance is a real repeat, not an overprint.
	joined := joinRunText([]TextRun{run("a", 10, 700), run("a", 15, 700)})

	if joined != "aa" {
		t.Fatalf("expected both letters kept, got %q", joined)
	}
}

func TestLineToleranceFallsBackForMissingFontSize(t *testing.T) {
	if got := lineTolerance(0); got != minLineTolerance {
		t.Fatalf("expected the minimum tolerance for an unknown font size, got %v", got)
	}
	if got := lineTolerance(20); got != 20*lineToleranceRatio {
		t.Fatalf("expected the tolerance to scale with the font size, got %v", got)
	}
}

func TestBuildSectionsFromRunsUsesFontSizeForHeadings(t *testing.T) {
	sections := buildSectionsFromRuns([]TextRun{
		run("Diagnosis", 10, 700, 18),
		run("The finding is normal.", 10, 680, 10),
		run("More detail follows here.", 10, 660, 10),
	})

	if len(sections) != 1 {
		t.Fatalf("expected one section, got %+v", sections)
	}
	if len(sections[0].Path) != 1 || sections[0].Path[0] != "Diagnosis" {
		t.Fatalf("expected the large line as the heading, got %+v", sections[0].Path)
	}
	if sections[0].Body != "The finding is normal.\nMore detail follows here." {
		t.Fatalf("body mismatch: %q", sections[0].Body)
	}
}

func TestBuildSectionsFromRunsRejoinsHyphenatedWords(t *testing.T) {
	sections := buildSectionsFromRuns([]TextRun{
		run("Heading here", 10, 700, 18),
		run("hyphen-", 10, 680, 10),
		run("ated word continues.", 10, 660, 10),
	})

	if len(sections) != 1 || sections[0].Body != "hyphenated word continues." {
		t.Fatalf("expected the hyphenated word rejoined, got %+v", sections)
	}
}

func TestNeedsSeparatorRespectsEncodedSpaces(t *testing.T) {
	// A space already supplied by either side means no separator is added, however far apart
	// the runs sit.
	if needsSeparator(run("left ", 10, 700), run("right", 300, 700)) {
		t.Fatal("a trailing space on the previous run should suppress the separator")
	}
	if needsSeparator(run("left", 10, 700), run(" right", 300, 700)) {
		t.Fatal("a leading space on the next run should suppress the separator")
	}
}

func TestSortLinesIntoReadingOrderBreaksTiesByLeft(t *testing.T) {
	lines := []textLine{
		{page: 0, top: 700, left: 200, text: "right"},
		{page: 0, top: 700, left: 10, text: "left"},
	}
	sortLinesIntoReadingOrder(lines)

	if lines[0].text != "left" {
		t.Fatalf("lines at the same height sort left to right, got %+v", lines)
	}
}
