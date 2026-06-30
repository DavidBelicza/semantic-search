package strategy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"semantic-search/internal/chunker"
	storage "semantic-search/internal/storage/sqlite"
)

func TestDefaultPoolSupportsMarkdownOnly(t *testing.T) {
	pool := DefaultPool()
	for _, path := range []string{"note.md", "note.markdown", "note.mdown", "NOTE.MD"} {
		if !pool.Supports(path) {
			t.Fatalf("expected default pool to support %q", path)
		}
	}

	if pool.Supports("note.txt") {
		t.Fatal("expected default pool to reject note.txt")
	}
}

func TestFileStrategyProcessesWithReaderParserAndChunker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fileStrategy, ok := DefaultPool().Find(path)
	if !ok {
		t.Fatal("expected markdown strategy")
	}
	fileStrategy.Chunker = chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1}

	chunks, err := fileStrategy.Process(context.Background(), storage.Document{AbsolutePath: path})
	if err != nil {
		t.Fatalf("process file: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: want 3, got %d", len(chunks))
	}
}

func TestProcessScannedDocumentsStoresChunksAndMarksChunked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:           42,
				FileID:       "1:100",
				AbsolutePath: path,
				Status:       storage.DocumentStatusScanned,
			},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 99, DocumentID: 42, ChunkIndex: 0, Text: "old"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abcdefg"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessScannedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process scanned documents: %v", err)
	}

	if result.Chunked != 1 {
		t.Fatalf("chunked count mismatch: want 1, got %d", result.Chunked)
	}
	if store.documents[0].Status != storage.DocumentStatusChunked {
		t.Fatalf("status mismatch: want chunked, got %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 3 {
		t.Fatalf("stored chunk count mismatch: want 3, got %d", len(store.chunks[42]))
	}
	if len(vectorStore.deleted) != 1 || vectorStore.deleted[0] != 99 {
		t.Fatalf("deleted vector ids mismatch: %#v", vectorStore.deleted)
	}
}

func TestProcessChunkedDocumentsStoresEmbeddingsAndMarksDone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:           42,
				FileID:       "1:100",
				AbsolutePath: path,
				Status:       storage.DocumentStatusChunked,
			},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "hello"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "world"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessChunkedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process chunked documents: %v", err)
	}

	if result.Embedded != 1 {
		t.Fatalf("embedded count mismatch: want 1, got %d", result.Embedded)
	}
	if store.documents[0].Status != storage.DocumentStatusDone {
		t.Fatalf("status mismatch: want done, got %q", store.documents[0].Status)
	}
	if len(vectorStore.embeddings) != 2 {
		t.Fatalf("embedding count mismatch: want 2, got %d", len(vectorStore.embeddings))
	}
}

type fakeReader struct {
	text string
}

func (r fakeReader) Read(ctx context.Context, document storage.Document) (string, error) {
	return r.text, nil
}

type fakeParser struct{}

func (p fakeParser) Parse(ctx context.Context, text string) (string, error) {
	return text, nil
}

type fakeEmbedder struct{}

func (e fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i), float32(len(texts[i]))}
	}
	return vectors, nil
}

type memoryStrategyStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
}

func (s *memoryStrategyStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
	var documents []storage.Document
	for _, document := range s.documents {
		if document.Status == status {
			documents = append(documents, document)
			if len(documents) == limit {
				return documents, nil
			}
		}
	}

	return documents, nil
}

func (s *memoryStrategyStore) ReplaceDocumentChunksAndStatus(ctx context.Context, documentID int64, chunks []storage.Chunk, status string) error {
	if s.chunks == nil {
		s.chunks = map[int64][]storage.Chunk{}
	}
	s.chunks[documentID] = chunks

	for i := range s.documents {
		if s.documents[i].ID == documentID {
			s.documents[i].Status = status
		}
	}

	return nil
}

func (s *memoryStrategyStore) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	return s.chunks[documentID], nil
}

func (s *memoryStrategyStore) UpdateDocumentStatus(ctx context.Context, fileID string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].Status = status
		}
	}

	return nil
}

type memoryVectorStore struct {
	deleted    []int64
	embeddings []storage.ChunkEmbedding
}

func (s *memoryVectorStore) Delete(ctx context.Context, chunkIDs []int64) error {
	s.deleted = append(s.deleted, chunkIDs...)
	return nil
}

func (s *memoryVectorStore) Replace(ctx context.Context, embeddings []storage.ChunkEmbedding) error {
	s.embeddings = append(s.embeddings, embeddings...)
	return nil
}
