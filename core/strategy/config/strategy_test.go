package config

import (
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
)

func TestFlatSectionsAndWholeSectionIgnoreEmptyInput(t *testing.T) {
	if got := flatSections("   \n\n  "); got != nil {
		t.Fatalf("want no sections, got %v", got)
	}
	if got := wholeSection(""); got != nil {
		t.Fatalf("want no parts, got %v", got)
	}
}

func TestSectionsOfFallsBackForAnUnknownExtension(t *testing.T) {
	store := configStrategy{maxTokens: 350}

	sections := store.sectionsOf("/p/a.unknown", "host = db01")

	if len(sections) != 1 || !strings.Contains(sections[0].Body, "db01") {
		t.Fatalf("got %v", sections)
	}
}

func newConfig() strategy.Strategy {
	return NewConfigStrategy(general.NewGeneralStrategy(nil, nil))
}

func chunksOf(t *testing.T, path, source string) []storage.Chunk {
	t.Helper()

	parsed, err := newConfig().Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chunks, err := newConfig().Chunk(storage.Document{AbsolutePath: path}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	return chunks
}

func joinedText(chunks []storage.Chunk) string {
	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString(chunk.Title + "\n" + chunk.Text + "\n")
	}

	return out.String()
}

func TestConfigDoesNotClaimTOML(t *testing.T) {
	if newConfig().Claims("Cargo.toml") {
		t.Fatal("should not claim .toml")
	}
}

func TestConfigTitlesChunksByKeyPath(t *testing.T) {
	source := strings.Join([]string{
		"database:",
		"  primary:",
		"    host: " + strings.Repeat("d", 2000),
		"cache:",
		"  size: " + strings.Repeat("c", 600),
	}, "\n")

	chunks := chunksOf(t, "/p/app.yaml", source)

	joined := joinedText(chunks)
	if !strings.Contains(joined, "database > primary") {
		t.Fatalf("want a key-path title, got:\n%s", joined)
	}
	if !strings.Contains(joined, "cache") {
		t.Fatalf("want the cache section, got:\n%s", joined)
	}
}

func TestConfigFallsBackToTheFileTitleForRootSettings(t *testing.T) {
	chunks := chunksOf(t, "/p/settings.ini", "retries = 3\n")

	if len(chunks) != 1 {
		t.Fatalf("want one chunk, got %d", len(chunks))
	}
	if chunks[0].Title != "settings" {
		t.Fatalf("want the file title, got %q", chunks[0].Title)
	}
}

func TestConfigFallsBackToTextWhenTheSourceDoesNotParse(t *testing.T) {
	chunks := chunksOf(t, "/p/broken.json", `{"host": "db01", `)

	if len(chunks) == 0 {
		t.Fatal("want the file indexed as text")
	}
	if !strings.Contains(joinedText(chunks), "db01") {
		t.Fatalf("want the raw text kept, got:\n%s", joinedText(chunks))
	}
}

func TestConfigChunkReturnsNothingForAnEmptyDocument(t *testing.T) {
	chunks, err := newConfig().Chunk(storage.Document{AbsolutePath: "/p/a.json"}, strategy.ParsedDocument{})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("want no chunks, got %d", len(chunks))
	}
}

func TestSectionsOfFallsBackWhenTheTreeYieldsNothing(t *testing.T) {
	store := configStrategy{maxTokens: 350}

	sections := store.sectionsOf("/p/a.ini", "no entries here")

	if len(sections) != 1 || !strings.Contains(sections[0].Body, "no entries here") {
		t.Fatalf("got %v", sections)
	}
}
