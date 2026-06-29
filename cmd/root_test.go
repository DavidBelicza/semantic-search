package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewRootCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd := NewRootCommand(&out, &fakeDocumentStore{})
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute root help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, appName) {
		t.Fatalf("help output does not contain app name %q: %q", appName, help)
	}

	for _, commandName := range []string{"index", "search"} {
		if !strings.Contains(help, commandName) {
			t.Fatalf("help output does not contain %q command: %q", commandName, help)
		}
	}

	if !strings.Contains(help, "--db") {
		t.Fatalf("help output does not contain db flag: %q", help)
	}
}
