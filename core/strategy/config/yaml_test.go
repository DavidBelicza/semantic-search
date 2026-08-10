package config

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

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

func TestParseYAMLHandlesSequencesOfScalars(t *testing.T) {
	got := render(t, parseYAML, "hosts:\n  - db01\n  - db02\n")

	if got != "hosts:\n  - db01\n  - db02" {
		t.Fatalf("got: %q", got)
	}
}

func TestParseYAMLHandlesAnEmptySource(t *testing.T) {
	got, err := parseYAML("")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Children) != 0 {
		t.Fatalf("want an empty tree, got %d children", len(got.Children))
	}
}

func TestYAMLDocumentHandlesAnEmptyDocumentNode(t *testing.T) {
	if got := yamlDocument(yaml.Node{}); len(got.Children) != 0 || got.Value != "" {
		t.Fatalf("want an empty node, got %+v", got)
	}
}

func TestYAMLAliasWithoutATargetYieldsTheKeyAlone(t *testing.T) {
	got := yamlAlias("prod", &yaml.Node{Kind: yaml.AliasNode})

	if got.Key != "prod" || len(got.Children) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseYAMLReportsMalformedSource(t *testing.T) {
	if _, err := parseYAML("a:\n- b\n  c: d\n"); err == nil {
		t.Fatal("want an error for malformed YAML")
	}
}
