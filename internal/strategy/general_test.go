package strategy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidbelicza/semantic-search/internal/storage"
)

type fakeEmbedder struct {
	inputs [][]string
}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.inputs = append(f.inputs, texts)
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(len(texts[i]))}
	}
	return vectors, nil
}

func TestGeneralStrategyClaimsEverything(t *testing.T) {
	if !NewGeneralStrategy(nil).Claims("/tmp/anything.bin") {
		t.Fatal("general strategy should claim every file")
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

	meta, err := NewGeneralStrategy(nil).CreateMetadata(FileRef{Path: path, Info: info})
	if err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if meta.AbsolutePath != filepath.Clean(path) {
		t.Fatalf("path mismatch: %q", meta.AbsolutePath)
	}
	if meta.SizeBytes != 5 {
		t.Fatalf("size mismatch: %d", meta.SizeBytes)
	}
	if meta.FileID == "" || meta.ModifiedAtNS == 0 {
		t.Fatalf("missing identity/mtime: %#v", meta)
	}
}

func TestGeneralStrategyFingerprintHashesContent(t *testing.T) {
	got := NewGeneralStrategy(nil).Fingerprint([]byte("hello"))
	if got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("fingerprint mismatch: %q", got)
	}
}

func TestGeneralStrategyParseReturnsRawText(t *testing.T) {
	got, err := NewGeneralStrategy(nil).Parse([]byte("plain text"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Sections) != 1 || got.Sections[0].Body != "plain text" {
		t.Fatalf("parse mismatch: %#v", got)
	}
}

func TestGeneralStrategyChunkSplitsByBudget(t *testing.T) {
	g := NewGeneralStrategy(nil)
	parsed, err := g.Parse([]byte(strings.Repeat("x", generalMaxTokens*4*2+1)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := g.Chunk(storage.Document{}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}
}

func TestGeneralStrategyEmbedFormatsAndDelegates(t *testing.T) {
	fake := &fakeEmbedder{}
	vectors, err := NewGeneralStrategy(fake).Embed(context.Background(), []storage.Chunk{{Title: "Heading", Text: "body"}})
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
