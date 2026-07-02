package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/ingest"
	storage "semantic-search/internal/storage/sqlite"
)

func TestDefaultMarkdownPoolSupportsMarkdownOnly(t *testing.T) {
	pool := NewPool(NewMarkdownStrategy())
	for _, path := range []string{"note.md", "note.markdown", "note.mdown", "NOTE.MD"} {
		if !pool.Supports(path) {
			t.Fatalf("expected pool to support %q", path)
		}
	}

	if pool.Supports("note.txt") {
		t.Fatal("expected pool to reject note.txt")
	}
}

func TestFindReturnsNoStrategyForUnsupportedExtension(t *testing.T) {
	pool := NewPool(NewMarkdownStrategy())
	if _, ok := pool.Find("note.txt"); ok {
		t.Fatal("expected no strategy for note.txt")
	}
}

func TestProcessRunsReadParseChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	strategy := fakeStrategy{text: "abcdefg", maxRunes: 3}

	// Exercise the interface steps the way the pipeline does: read → parse → chunk.
	text, err := strategy.Read(context.Background(), storage.Document{AbsolutePath: path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	parsed, err := strategy.Parse(context.Background(), text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chunks, err := strategy.Chunk(context.Background(), storage.Document{AbsolutePath: path}, parsed)
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}
}

// fakeStrategy is a controllable Strategy: a fixed read text, identity parse, and a
// fixed-window chunker.
type fakeStrategy struct {
	text     string
	maxRunes int
}

func (fakeStrategy) Extensions() []string { return []string{".md"} }

func (fakeStrategy) Discovery(rootPath string, options ingest.Options) ([]storage.FileMetadata, error) {
	return nil, nil
}

func (fakeStrategy) Registration(ctx context.Context, store ingest.MetadataStore, files []storage.FileMetadata) error {
	return nil
}

func (fakeStrategy) Fingerprinting(ctx context.Context, store ingest.Store, failFast bool) error {
	return nil
}

func (s fakeStrategy) Read(ctx context.Context, doc storage.Document) (string, error) {
	return s.text, nil
}

func (s fakeStrategy) Parse(ctx context.Context, text string) (string, error) {
	return text, nil
}

func (s fakeStrategy) Chunk(ctx context.Context, doc storage.Document, text string) ([]storage.Chunk, error) {
	return fixedWindowChunks(text, s.maxRunes), nil
}

// fixedWindowChunks splits text into fixed-size rune windows, hashing each like the real
// chunker so reconciliation-by-content-hash behaves the same.
func fixedWindowChunks(text string, maxRunes int) []storage.Chunk {
	runes := []rune(text)
	var chunks []storage.Chunk
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		piece := string(runes[start:end])
		sum := sha256.Sum256([]byte(piece))
		chunks = append(chunks, storage.Chunk{
			ChunkIndex:  len(chunks),
			Text:        piece,
			StartOffset: start,
			EndOffset:   end,
			ContentHash: hex.EncodeToString(sum[:]),
		})
	}

	return chunks
}
