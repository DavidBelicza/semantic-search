package config

import (
	"strings"
	"testing"
)

func TestConfigClaimsOnlyConfigExtensions(t *testing.T) {
	s := newConfig()
	for _, path := range []string{"a.json", "a.xml", "a.yaml", "a.yml", "a.ini", "a.properties", "A.JSON", "A.Yml"} {
		if !s.Claims(path) {
			t.Fatalf("should claim %q", path)
		}
	}
	for _, path := range []string{"a.md", "a.txt", "a.go", "a.html", "a.docx", "noext"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q", path)
		}
	}
}

func TestConfigNeverClaimsXHTML(t *testing.T) {
	s := newConfig()
	for _, path := range []string{"a.xhtml", "a.XHTML", "page.rss.xhtml"} {
		if s.Claims(path) {
			t.Fatalf("should not claim %q, it is the HTML strategy's", path)
		}
	}
	if !s.Claims("feed.rss.xml") {
		t.Fatal("should claim a multi-dot .xml file")
	}
}

func TestConfigIndexesEveryFormatItClaims(t *testing.T) {
	cases := []struct{ path, source, want string }{
		{"/p/a.json", `{"port":8080}`, "port = 8080"},
		{"/p/a.xml", `<cfg><port>8080</port></cfg>`, "port = 8080"},
		{"/p/a.yaml", "port: 8080", "port = 8080"},
		{"/p/a.ini", "[server]\nport = 8080", "port = 8080"},
		{"/p/a.properties", "server.port=8080", "port = 8080"},
	}

	for _, testCase := range cases {
		joined := joinedText(chunksOf(t, testCase.path, testCase.source))
		if !strings.Contains(joined, testCase.want) {
			t.Fatalf("%s: want %q in:\n%s", testCase.path, testCase.want, joined)
		}
	}
}
