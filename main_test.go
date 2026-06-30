package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"semantic-search/cmd"
)

func TestRunShowsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dbPath := filepath.Join(t.TempDir(), "index.db")

	if err := run([]string{"--db", dbPath, "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}

	if !strings.Contains(stdout.String(), "semantic-search") {
		t.Fatalf("help output does not contain app name: %q", stdout.String())
	}

	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestDatabasePathFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "default",
			args: []string{"index", "."},
			want: cmd.DefaultDatabasePath,
		},
		{
			name: "separate value",
			args: []string{"--db", "custom.db", "index", "."},
			want: "custom.db",
		},
		{
			name: "equals value",
			args: []string{"index", "--db=custom.db", "."},
			want: "custom.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := databasePathFromArgs(tt.args)
			if err != nil {
				t.Fatalf("database path from args: %v", err)
			}
			if got != tt.want {
				t.Fatalf("database path mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestVectorPathFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "default",
			args: []string{"index", "."},
			want: cmd.DefaultVectorPath,
		},
		{
			name: "separate value",
			args: []string{"--vector", "/tmp/vectorlite.dylib", "index", "."},
			want: "/tmp/vectorlite.dylib",
		},
		{
			name: "equals value",
			args: []string{"index", "--vector=/tmp/vectorlite.dylib", "."},
			want: "/tmp/vectorlite.dylib",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vectorPathFromArgs(tt.args)
			if err != nil {
				t.Fatalf("vector path from args: %v", err)
			}
			if got != tt.want {
				t.Fatalf("vector path mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeExtensionPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "mac dylib",
			path: "./vector-db.dylib",
			want: "./vector-db",
		},
		{
			name: "linux shared object",
			path: "./vector-db.so",
			want: "./vector-db",
		},
		{
			name: "windows dll",
			path: "./vector-db.dll",
			want: "./vector-db",
		},
		{
			name: "already normalized",
			path: "./vector-db",
			want: "./vector-db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeExtensionPath(tt.path)
			if got != tt.want {
				t.Fatalf("normalized path mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}
