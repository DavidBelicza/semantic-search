package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewScanCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	scanCmd := NewScanCommand(&out, &fakeDocumentStore{})
	scanCmd.SetArgs([]string{"--help"})

	if err := scanCmd.Execute(); err != nil {
		t.Fatalf("execute scan help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "scan") {
		t.Fatalf("help output does not contain scan usage: %q", help)
	}
}

func TestNewScanCommandRejectsArgs(t *testing.T) {
	var out bytes.Buffer
	scanCmd := NewScanCommand(&out, &fakeDocumentStore{})
	scanCmd.SetArgs([]string{"extra"})

	if err := scanCmd.Execute(); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}
