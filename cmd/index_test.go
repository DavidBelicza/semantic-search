package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/storage/sqlitevec"
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


type fakeDocumentStore struct{}

func (s *fakeDocumentStore) UpsertDocuments(ctx context.Context, files []storage.FileMetadata) error {
	return nil
}

func (s *fakeDocumentStore) DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
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

func (s *fakeDocumentStore) MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error {
	return nil
}

func (s *fakeDocumentStore) ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	return plan.Insert, nil
}

func (s *fakeDocumentStore) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	return nil, nil
}

func (s *fakeDocumentStore) ChunkMetadataByIDs(ctx context.Context, chunkIDs []int64) ([]storage.ChunkMetadata, error) {
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

func (s *fakeVectorStore) Search(ctx context.Context, query []float32, limit int) ([]sqlitevec.VectorHit, error) {
	return nil, nil
}
