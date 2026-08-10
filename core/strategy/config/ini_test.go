package config

import (
	"strings"
	"testing"
)

func TestParseININestsSectionsAndDottedKeys(t *testing.T) {
	source := strings.Join([]string{
		"[db.primary]",
		"; the main host",
		"host = localhost",
		"port: 5432",
	}, "\n")

	got := render(t, parseINI, source)

	want := strings.Join([]string{
		"db:",
		"  primary:",
		"    # the main host",
		"    host = localhost",
		"    port = 5432",
	}, "\n")

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseININestsPropertiesStyleKeys(t *testing.T) {
	got := render(t, parseINI, "server.http.port=8080\nserver.http.host=0.0.0.0\n")

	want := "server:\n  http:\n    port = 8080\n    host = 0.0.0.0"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseINIIgnoresBlankAndUnparsableLines(t *testing.T) {
	got := render(t, parseINI, "\n\nnot an entry\n\nkey = value\n")

	if got != "key = value" {
		t.Fatalf("got: %q", got)
	}
}

func TestParseINIJoinsConsecutiveCommentLines(t *testing.T) {
	got := render(t, parseINI, "# first\n# second\nkey = value\n")

	if got != "# first\n# second\nkey = value" {
		t.Fatalf("got: %q", got)
	}
}

func TestParseINIDropsACommentSeparatedByABlankLine(t *testing.T) {
	got := render(t, parseINI, "# orphan\n\nkey = value\n")

	if got != "key = value" {
		t.Fatalf("got: %q", got)
	}
}

func TestSplitKeyPathFallsBackToTheWholeKey(t *testing.T) {
	got := splitKeyPath("..")

	if len(got) != 1 || got[0] != ".." {
		t.Fatalf("got %v", got)
	}
}
