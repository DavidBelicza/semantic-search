package config

import (
	"strings"
	"testing"
)

// render parses a source with the given parser and renders the tree, so a test can assert the
// decoded shape as text.
func render(t *testing.T, parse parseFunc, source string) string {
	t.Helper()

	root, err := parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return renderSubtree(root)
}

func TestParseJSONKeepsKeyOrderAndNesting(t *testing.T) {
	got := render(t, parseJSON, `{"server":{"host":"localhost","port":8080},"debug":true,"tags":["a","b"]}`)

	want := strings.Join([]string{
		"server:",
		"  host = localhost",
		"  port = 8080",
		"debug = true",
		"tags:",
		"  - a",
		"  - b",
	}, "\n")

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Decoding into map[string]any would randomize key order between runs and make chunk order
// unstable, so the parser reads the token stream instead. This asserts the order is the file's.
func TestParseJSONOrderIsStableAcrossRuns(t *testing.T) {
	source := `{"z":1,"a":2,"m":3,"b":4,"y":5,"c":6}`
	first := render(t, parseJSON, source)

	for i := 0; i < 20; i++ {
		if got := render(t, parseJSON, source); got != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, got, first)
		}
	}

	if !strings.HasPrefix(first, "z = 1\na = 2\nm = 3") {
		t.Fatalf("order is not the file's: %s", first)
	}
}

func TestParseJSONRendersScalarKinds(t *testing.T) {
	got := render(t, parseJSON, `{"s":"x","n":1.5,"b":false,"nil":null}`)

	want := "s = x\nn = 1.5\nb = false\nnil = null"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseXMLNestsElementsAndFoldsAttributes(t *testing.T) {
	got := render(t, parseXML, `<config><!-- the primary database --><db name="primary"><host>db01</host></db></config>`)

	want := strings.Join([]string{
		"config:",
		"  # the primary database",
		"  db:",
		"    @name = primary",
		"    host = db01",
	}, "\n")

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// An element carrying both attributes and text would lose its text if the value were dropped
// for having children, so mixed content is kept as an anonymous leaf.
func TestParseXMLKeepsTextOfElementWithAttributes(t *testing.T) {
	got := render(t, parseXML, `<a href="/x">click here</a>`)

	want := "a:\n  @href = /x\n  - click here"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseYAMLKeepsCommentsAndNesting(t *testing.T) {
	source := strings.Join([]string{
		"# how long before a session expires",
		"timeout: 30",
		"cache:",
		"  size: 100 # entries",
	}, "\n")

	got := render(t, parseYAML, source)

	want := strings.Join([]string{
		"# how long before a session expires",
		"timeout = 30",
		"cache:",
		"  # entries",
		"  size = 100",
	}, "\n")

	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseYAMLKeepsEveryDocumentOfAStream(t *testing.T) {
	got := render(t, parseYAML, "kind: Service\n---\nkind: Deployment\n")

	want := "-\n  kind = Service\n-\n  kind = Deployment"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseYAMLResolvesAliases(t *testing.T) {
	source := "base: &b\n  retries: 3\nprod: *b\n"

	got := render(t, parseYAML, source)
	if !strings.Contains(got, "prod:\n  retries = 3") {
		t.Fatalf("alias not resolved: %s", got)
	}
}

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

// .properties has no section headers; its dotted keys carry the nesting instead.
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

func TestParsersReportMalformedSource(t *testing.T) {
	if _, err := parseJSON(`{"a":`); err == nil {
		t.Fatal("want an error for truncated JSON")
	}
	if _, err := parseYAML("a:\n- b\n  c: d\n"); err == nil {
		t.Fatal("want an error for malformed YAML")
	}
}
