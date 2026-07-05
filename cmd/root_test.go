package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestExecuteShowsHelpWithCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("execute help: %v", err)
	}

	help := stdout.String()
	if !strings.Contains(help, appName) {
		t.Fatalf("help missing app name: %q", help)
	}
	for _, name := range []string{"index", "search", "--db"} {
		if !strings.Contains(help, name) {
			t.Fatalf("help missing %q: %q", name, help)
		}
	}
}

func TestExecuteIndexRequiresPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute([]string{"index"}, &stdout, &stderr); err == nil {
		t.Fatal("expected error when index path is missing")
	}
}

func TestExecuteSearchRequiresLimitAndQuery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute([]string{"search", "only-one-arg"}, &stdout, &stderr); err == nil {
		t.Fatal("expected error when search args are incomplete")
	}
}
