package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"semantic-search/internal/chunker"
	"semantic-search/internal/crawler"
	"semantic-search/internal/parser"
	"semantic-search/internal/reader"
	"semantic-search/internal/storage"
	"semantic-search/internal/strategy"
)

func TestNewIndexCommandShowsHelp(t *testing.T) {
	var out bytes.Buffer
	indexCmd := NewIndexCommand(&out, &fakeDocumentStore{}, &fakeVectorStore{})
	indexCmd.SetArgs([]string{"--help"})

	if err := indexCmd.Execute(); err != nil {
		t.Fatalf("execute index help: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "index [path]") {
		t.Fatalf("help output does not contain index usage: %q", help)
	}
}

func TestNewIndexCommandRequiresPath(t *testing.T) {
	var out bytes.Buffer
	indexCmd := NewIndexCommand(&out, &fakeDocumentStore{}, &fakeVectorStore{})
	indexCmd.SetArgs([]string{})

	if err := indexCmd.Execute(); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestNewIndexCommandStoresMetadataAndScansContent(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	nested := filepath.Join(root, "notes", "daily")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}

	readmeFile := filepath.Join(root, "README.md")
	planFile := filepath.Join(root, "notes", "plan.md")
	entryFile := filepath.Join(nested, "entry.md")
	ignoredFile := filepath.Join(root, "ignore.txt")

	files := []string{readmeFile, planFile, entryFile, ignoredFile}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatalf("write test file %q: %v", file, err)
		}
	}

	var out bytes.Buffer
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	vectorStore := &fakeVectorStore{}
	indexCmd := NewIndexCommandWithPool(&out, store, vectorStore, fakeIndexStrategyPool())
	indexCmd.SetArgs([]string{root})

	if err := indexCmd.Execute(); err != nil {
		t.Fatalf("execute index: %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("expected no index output, got %q", out.String())
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if count != 3 {
		t.Fatalf("document count mismatch: want 3, got %d", count)
	}

	var doneCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE content_hash IS NOT NULL AND status = ?", storage.DocumentStatusDone).Scan(&doneCount); err != nil {
		t.Fatalf("count done documents: %v", err)
	}
	if doneCount != 3 {
		t.Fatalf("done document count mismatch: want 3, got %d", doneCount)
	}

	var chunkCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", chunkCount)
	}

	if len(vectorStore.embeddings) != 3 {
		t.Fatalf("embedding count mismatch: want 3, got %d", len(vectorStore.embeddings))
	}
}

type fakeDocumentStore struct{}

func (s *fakeDocumentStore) UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error {
	return nil
}

func (s *fakeDocumentStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
	return nil, nil
}

func (s *fakeDocumentStore) UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error {
	return nil
}

func (s *fakeDocumentStore) UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error {
	return nil
}

func (s *fakeDocumentStore) UpdateDocumentStatus(ctx context.Context, fileID string, status string) error {
	return nil
}

func (s *fakeDocumentStore) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error {
	return nil
}

func (s *fakeDocumentStore) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	return nil, nil
}

type fakeVectorStore struct {
	deleted    []int64
	embeddings []storage.ChunkEmbedding
}

func (s *fakeVectorStore) Delete(ctx context.Context, chunkIDs []int64) error {
	s.deleted = append(s.deleted, chunkIDs...)
	return nil
}

func (s *fakeVectorStore) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	s.embeddings = append(s.embeddings, embeddings...)
	return nil
}

type fakeIndexEmbedder struct{}

func (e fakeIndexEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i), float32(len(texts[i]))}
	}
	return vectors, nil
}

func fakeIndexStrategyPool() strategy.Pool {
	return strategy.Pool{
		{
			Extensions: []string{".md", ".markdown", ".mdown"},
			Reader:     reader.MarkdownReader{},
			Parser:     parser.MarkdownParser{},
			Chunker:    chunker.NewHardLimitChunker(chunker.DefaultMaxTokens),
			Embedder:   fakeIndexEmbedder{},
		},
	}
}
