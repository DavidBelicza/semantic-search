package config

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestParseJSONReportsAnEmptySource(t *testing.T) {
	if _, err := parseJSON(""); err == nil {
		t.Fatal("want an error for an empty source")
	}
}

func TestParseJSONReportsATruncatedArray(t *testing.T) {
	if _, err := parseJSON(`[1,`); err == nil {
		t.Fatal("want an error for a truncated array")
	}
	if _, err := parseJSON(`{"a":[{"b":`); err == nil {
		t.Fatal("want an error for a truncated object inside an array")
	}
}

func TestJSONScalarRendersAnUnexpectedTokenAsEmpty(t *testing.T) {
	if got := jsonScalar(json.Token(struct{}{})); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParseJSONNestsArraysOfObjects(t *testing.T) {
	got := render(t, parseJSON, `{"users":[{"name":"ada"},{"name":"alan"}]}`)

	want := "users:\n  -\n    name = ada\n  -\n    name = alan"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseJSONReportsATruncatedObject(t *testing.T) {
	if _, err := parseJSON(`{"a":`); err == nil {
		t.Fatal("want an error for truncated JSON")
	}
}
