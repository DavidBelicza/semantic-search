package general

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

type fakeEmbedder struct {
	inputs [][]string
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.inputs = append(f.inputs, texts)
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(len(texts[i]))}
	}
	return vectors, nil
}

type fakeModel struct{}

func (fakeModel) Name() string    { return "fake" }
func (fakeModel) Dimensions() int { return 1 }
func (fakeModel) BuildData(chunk storage.Chunk) string {
	return "title: " + chunk.Title + " | text: " + chunk.Text
}
func (fakeModel) BuildQuery(query, taskType string) (string, error) { return query, nil }

func TestGeneralStrategyClaimsPlainText(t *testing.T) {
	s := NewGeneralStrategy(nil, nil)

	for _, path := range []string{"/tmp/note.txt", "/tmp/a.text", "/tmp/run.log", "/tmp/doc.rst", "/tmp/notes.org", "/tmp/page.adoc", "/tmp/UPPER.TXT"} {
		if !s.Claims(path) {
			t.Fatalf("general strategy should claim plain-text file %q", path)
		}
	}

	for _, path := range []string{"/tmp/anything.bin", "/tmp/page.md", "/tmp/doc.pdf", "/tmp/noext"} {
		if s.Claims(path) {
			t.Fatalf("general strategy should not claim non-plain-text file %q", path)
		}
	}
}

func TestGeneralStrategyCreateMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	meta, err := NewGeneralStrategy(nil, nil).CreateMetadata(strategy.FileRef{Path: path, Info: info})
	if err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if meta.AbsolutePath != filepath.Clean(path) || meta.SizeBytes != 5 {
		t.Fatalf("metadata mismatch: %#v", meta)
	}
	if meta.FileID == "" || meta.ModifiedAtNS == 0 {
		t.Fatalf("missing identity/mtime: %#v", meta)
	}
}

func TestGeneralStrategyFingerprintHashesContent(t *testing.T) {
	got := NewGeneralStrategy(nil, nil).Fingerprint([]byte("hello"))
	if got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("fingerprint mismatch: %q", got)
	}
}

func TestGeneralStrategyParseReturnsWholeText(t *testing.T) {
	got, err := NewGeneralStrategy(nil, nil).Parse([]byte("plain text"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Sections) != 1 || got.Sections[0].Body != "plain text" {
		t.Fatalf("parse mismatch: %#v", got)
	}
}

func TestGeneralStrategyChunkProducesMultipleChunks(t *testing.T) {
	g := NewGeneralStrategy(nil, nil)
	parsed, err := g.Parse([]byte(strings.Repeat("x", 4000)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := g.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestGeneralStrategyEmbedFormatsAndDelegates(t *testing.T) {
	fake := &fakeEmbedder{}
	vectors, err := NewGeneralStrategy(fakeModel{}, fake).Embed(context.Background(), []storage.Chunk{{Title: "Heading", Text: "body"}})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("vector count mismatch: %d", len(vectors))
	}
	if len(fake.inputs) != 1 || !strings.Contains(fake.inputs[0][0], "title: Heading | text: body") {
		t.Fatalf("embed input not formatted: %#v", fake.inputs)
	}
}

func TestFileTitleFromPath(t *testing.T) {
	cases := map[string]string{
		"/a/b/notes.md": "notes",
		"report.pdf":    "report",
		"noext":         "noext",
		"":              "",
	}
	for in, want := range cases {
		if got := FileTitleFromPath(in); got != want {
			t.Fatalf("FileTitleFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSectionTitleJoinsPathOrFallsBack(t *testing.T) {
	if got := sectionTitle([]string{"Guide", "Payments"}, "file"); got != "Guide > Payments" {
		t.Fatalf("joined path mismatch: %q", got)
	}
	if got := sectionTitle(nil, "file"); got != "file" {
		t.Fatalf("fallback mismatch: %q", got)
	}
}
