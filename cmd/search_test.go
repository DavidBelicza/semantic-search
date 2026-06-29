package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewSearchCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	searchCmd := NewSearchCommand(&out)
	searchCmd.SetArgs([]string{"--help"})

	if err := searchCmd.Execute(); err != nil {
		t.Fatalf("execute search help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "search [query]") {
		t.Fatalf("help output does not contain search usage: %q", help)
	}
}

func TestNewSearchCommandRequiresQuery(t *testing.T) {
	var out bytes.Buffer
	searchCmd := NewSearchCommand(&out)
	searchCmd.SetArgs([]string{})

	if err := searchCmd.Execute(); err == nil {
		t.Fatal("expected missing query error")
	}
}
