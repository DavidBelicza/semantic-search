package subtitle

import (
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func newSubtitle() strategy.Strategy {
	return NewSubtitleStrategy(general.NewGeneralStrategy(nil, nil))
}

func parseBody(t *testing.T, source string) string {
	t.Helper()

	parsed, err := newSubtitle().Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections) != 1 {
		t.Fatalf("want 1 section, got %d", len(parsed.Sections))
	}

	return parsed.Sections[0].Body
}

func TestClaimsOnlySubtitleExtensions(t *testing.T) {
	s := newSubtitle()

	for _, path := range []string{"a.srt", "a.vtt", "A.SRT", "A.Vtt"} {
		if !s.Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	for _, path := range []string{"a.txt", "a.md", "a.sub", "a.ass", "a.ssa", "noext"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q", path)
		}
	}
}

func TestParseKeepsSpokenLinesInOrder(t *testing.T) {
	body := parseBody(t, "1\n00:00:01,000 --> 00:00:03,000\nFirst line.\nSecond line.\n\n"+
		"2\n00:00:03,000 --> 00:00:05,000\nThird line.\n")

	want := "First line.\nSecond line.\nThird line."
	if body != want {
		t.Fatalf("want %q, got %q", want, body)
	}
}

func TestParseKeepsRepeatedLines(t *testing.T) {
	body := parseBody(t, "1\n00:00:01,000 --> 00:00:02,000\nStay back.\n\n"+
		"2\n00:00:02,000 --> 00:00:03,000\nStay back.\n")

	want := "Stay back.\nStay back."
	if body != want {
		t.Fatalf("want %q, got %q", want, body)
	}
}

func TestParseSectionHasNoHeadingPath(t *testing.T) {
	parsed, err := newSubtitle().Parse([]byte("1\n00:00:01,000 --> 00:00:02,000\nOnly line.\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Sections[0].Path) != 0 {
		t.Fatalf("want empty path, got %v", parsed.Sections[0].Path)
	}
}

func TestParseWithoutCuesReturnsNoSections(t *testing.T) {
	for _, source := range []string{"", "WEBVTT\n\nNOTE nothing here\n", "just prose\nwith no timings\n"} {
		parsed, err := newSubtitle().Parse([]byte(source))
		if err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
		if len(parsed.Sections) != 0 {
			t.Fatalf("source %q: want no sections, got %d", source, len(parsed.Sections))
		}
	}
}

func TestParseNormalizesLineEndingsAndByteOrderMark(t *testing.T) {
	body := parseBody(t, "\ufeff1\r\n00:00:01,000 --> 00:00:02,000\r\nWindows line.\r\n")

	if body != "Windows line." {
		t.Fatalf("want %q, got %q", "Windows line.", body)
	}
}

func TestChunkIsInheritedAndTitledFromFileName(t *testing.T) {
	s := newSubtitle()

	parsed, err := s.Parse([]byte("1\n00:00:01,000 --> 00:00:02,000\nA short line.\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chunks, err := s.Chunk(storage.Document{AbsolutePath: "/films/documentary.srt"}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Title != "documentary" {
		t.Fatalf("want title %q, got %q", "documentary", chunks[0].Title)
	}
	if chunks[0].Text != "A short line." {
		t.Fatalf("want text %q, got %q", "A short line.", chunks[0].Text)
	}
}
