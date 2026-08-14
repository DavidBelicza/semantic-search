package subtitle

import "testing"

func TestBuildTranscriptDropsNumberingAndTimings(t *testing.T) {
	source := "1\n00:00:01,000 --> 00:00:03,000\nOpen the gate.\n\n2\n00:00:03,000 --> 00:00:05,000\nIt is jammed.\n"

	if got := buildTranscript(source); got != "Open the gate.\nIt is jammed." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptDropsWebVTTPreamble(t *testing.T) {
	source := "WEBVTT\n\nNOTE recorded on location\n\nSTYLE\n::cue { color: red }\n\n" +
		"opening\n00:00:02.000 --> 00:00:04.000\nThe tide is turning.\n"

	if got := buildTranscript(source); got != "The tide is turning." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptStripsMarkupAndOverrides(t *testing.T) {
	source := "1\n00:00:01,000 --> 00:00:02,000\n<v Ana>She <i>knew</i> the answer.\n\n" +
		"2\n00:00:02,000 --> 00:00:03,000\n{\\an8}Look up.\n"

	if got := buildTranscript(source); got != "She knew the answer.\nLook up." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptDecodesEntitiesAfterStrippingTags(t *testing.T) {
	source := "1\n00:00:01,000 --> 00:00:02,000\nSalt &amp; pepper\n\n" +
		"2\n00:00:02,000 --> 00:00:03,000\n&lt;i&gt; stays text\n"

	if got := buildTranscript(source); got != "Salt & pepper\n<i> stays text" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptSkipsBlocksWithoutTiming(t *testing.T) {
	source := "no timing here\n\n1\n00:00:01,000 --> 00:00:02,000\nKept line.\n"

	if got := buildTranscript(source); got != "Kept line." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptSkipsCuesWithNoText(t *testing.T) {
	source := "1\n00:00:01,000 --> 00:00:02,000\n\n\n2\n00:00:02,000 --> 00:00:03,000\n<i></i>\n\n" +
		"3\n00:00:03,000 --> 00:00:04,000\nOnly this survives.\n"

	if got := buildTranscript(source); got != "Only this survives." {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTranscriptReturnsEmptyWithoutCues(t *testing.T) {
	for _, source := range []string{"", "WEBVTT\n", "one\ntwo\n"} {
		if got := buildTranscript(source); got != "" {
			t.Fatalf("source %q: got %q", source, got)
		}
	}
}

func TestSpokenLinesReturnsNilWithoutTimingLine(t *testing.T) {
	if got := spokenLines("1\nsome text"); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestTimingLineIndexFindsAndMisses(t *testing.T) {
	if got := timingLineIndex([]string{"1", "00:00:01,000 --> 00:00:02,000", "text"}); got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
	if got := timingLineIndex([]string{"1", "text"}); got != -1 {
		t.Fatalf("want -1, got %d", got)
	}
}

func TestCleanLinesDropsBlankAndWhitespaceOnlyLines(t *testing.T) {
	got := cleanLines([]string{"", "  ", "Kept.", "\t", "Also kept."})

	if len(got) != 2 || got[0] != "Kept." || got[1] != "Also kept." {
		t.Fatalf("got %v", got)
	}
}

func TestCleanLineTrimsStripsAndDecodes(t *testing.T) {
	cases := map[string]string{
		"  padded  ":        "padded",
		"<b>bold</b>":       "bold",
		"{\\an8}positioned": "positioned",
		"a &amp; b":         "a & b",
		"<i></i>":           "",
	}

	for input, want := range cases {
		if got := cleanLine(input); got != want {
			t.Fatalf("input %q: want %q, got %q", input, want, got)
		}
	}
}
