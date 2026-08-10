package config

import (
	"strings"
	"testing"
)

func render(t *testing.T, parse parseFunc, source string) string {
	t.Helper()

	root, err := parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return renderSubtree(root)
}

func TestConfigRendersEveryFormatTheSameWay(t *testing.T) {
	sources := map[string]string{
		"/p/a.json": `{"server":{"port":8080}}`,
		"/p/a.yaml": "server:\n  port: 8080",
		"/p/a.xml":  "<server><port>8080</port></server>",
		"/p/a.ini":  "[server]\nport = 8080",
	}

	for path, source := range sources {
		chunks := chunksOf(t, path, source)
		if len(chunks) != 1 {
			t.Fatalf("%s: want one chunk, got %d", path, len(chunks))
		}
		if !strings.Contains(chunks[0].Text, "port = 8080") {
			t.Fatalf("%s: rendered differently: %q", path, chunks[0].Text)
		}
	}
}
