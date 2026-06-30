package strategy

import (
	"context"
	"errors"
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

func TestProcessScannedDocumentsReconcilesChunksEmbedsAndMarksEmbedded(t *testing.T) {
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
				{ID: 99, DocumentID: 42, ChunkIndex: 0, Text: "old", ContentHash: "old"},
			},
		},
		nextChunkID: 100,
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

	if result.Processed != 1 {
		t.Fatalf("processed count mismatch: want 1, got %d", result.Processed)
	}
	if result.Embedded != 1 {
		t.Fatalf("embedded count mismatch: want 1, got %d", result.Embedded)
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: want embedded, got %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 3 {
		t.Fatalf("stored chunk count mismatch: want 3, got %d", len(store.chunks[42]))
	}
	if len(vectorStore.deleted) != 1 || vectorStore.deleted[0] != 99 {
		t.Fatalf("deleted vector ids mismatch: %#v", vectorStore.deleted)
	}
	if len(vectorStore.embeddings) != 3 {
		t.Fatalf("embedding count mismatch: want 3, got %d", len(vectorStore.embeddings))
	}
}

func TestProcessScannedDocumentsKeepsUnchangedChunksAndEmbedsNewChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:                  42,
				FileID:              "1:100",
				AbsolutePath:        path,
				Status:              storage.DocumentStatusScanned,
				ContentHash:         "new-content-hash",
				EmbeddedContentHash: "old-content-hash",
			},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
			},
		},
		nextChunkID: 101,
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abcdef"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessScannedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process scanned documents: %v", err)
	}

	if result.Embedded != 1 {
		t.Fatalf("embedded count mismatch: want 1, got %d", result.Embedded)
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: want embedded, got %q", store.documents[0].Status)
	}
	if len(vectorStore.deleted) != 0 {
		t.Fatalf("expected no deleted vectors, got %#v", vectorStore.deleted)
	}
	if len(vectorStore.embeddings) != 1 {
		t.Fatalf("embedding count mismatch: want 1, got %d", len(vectorStore.embeddings))
	}
	if vectorStore.embeddings[0].ChunkID == 100 {
		t.Fatalf("expected only new chunk to be embedded, got existing chunk id %d", vectorStore.embeddings[0].ChunkID)
	}
}

func TestProcessScannedDocumentsEmbedsAllChunksWhenScannedDocumentHasNoChunkChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
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
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "cb8379ac2098aa165029e3938a51da0bcecfc008fd6795f401178647f96c5b34"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abcdef"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessScannedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process scanned documents: %v", err)
	}

	if result.Embedded != 1 {
		t.Fatalf("embedded count mismatch: want 1, got %d", result.Embedded)
	}
	if len(vectorStore.embeddings) != 2 {
		t.Fatalf("embedding count mismatch: want 2, got %d", len(vectorStore.embeddings))
	}
	if vectorStore.embeddings[0].ChunkID != 100 || vectorStore.embeddings[1].ChunkID != 101 {
		t.Fatalf("embedded chunk ids mismatch: %#v", vectorStore.embeddings)
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: want embedded, got %q", store.documents[0].Status)
	}
}

func TestProcessScannedDocumentsSkipsReembeddingWhenAlreadyEmbeddedAndUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:                  42,
				FileID:              "1:100",
				AbsolutePath:        path,
				Status:              storage.DocumentStatusScanned,
				ContentHash:         "content-hash",
				EmbeddedContentHash: "content-hash",
			},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "cb8379ac2098aa165029e3938a51da0bcecfc008fd6795f401178647f96c5b34"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abcdef"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessScannedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process scanned documents: %v", err)
	}

	if result.Embedded != 0 {
		t.Fatalf("expected no re-embed, got embedded count %d", result.Embedded)
	}
	if len(vectorStore.embeddings) != 0 {
		t.Fatalf("expected no embeddings, got %#v", vectorStore.embeddings)
	}
	if len(vectorStore.deleted) != 0 {
		t.Fatalf("expected no deleted vectors, got %#v", vectorStore.deleted)
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: want embedded, got %q", store.documents[0].Status)
	}
}

func TestProcessChunkedDocumentsEmbedsAndMarksEmbedded(t *testing.T) {
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
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "hash-1"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "hash-2"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Embedder:   fakeEmbedder{},
		},
	}

	result, err := ProcessChunkedDocuments(context.Background(), store, vectorStore, pool)
	if err != nil {
		t.Fatalf("process chunked documents: %v", err)
	}

	if result.Processed != 1 || result.Embedded != 1 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if len(vectorStore.embeddings) != 2 {
		t.Fatalf("embedding count mismatch: want 2, got %d", len(vectorStore.embeddings))
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: want embedded, got %q", store.documents[0].Status)
	}
}

func TestProcessScannedDocumentsLeavesDocumentChunkedWhenEmbeddingFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStrategyStore{
		documents: []storage.Document{
			{
				ID:           42,
				FileID:       "1:100",
				AbsolutePath: path,
				Status:       storage.DocumentStatusScanned,
			},
		},
		nextChunkID: 100,
	}
	pool := Pool{
		{
			Extensions: []string{".md"},
			Reader:     fakeReader{text: "abc"},
			Parser:     fakeParser{},
			Chunker:    chunker.HardLimitChunker{MaxTokens: 3, AverageTokenLength: 1},
			Embedder:   fakeFailingEmbedder{},
		},
	}

	_, err := ProcessScannedDocuments(context.Background(), store, &memoryVectorStore{}, pool)
	if err == nil {
		t.Fatal("expected embedding error")
	}
	if store.documents[0].Status != storage.DocumentStatusChunked {
		t.Fatalf("status mismatch: want chunked, got %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 1 {
		t.Fatalf("stored chunk count mismatch: want 1, got %d", len(store.chunks[42]))
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

type fakeFailingEmbedder struct{}

func (e fakeFailingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("embedding failed")
}

type memoryStrategyStore struct {
	documents   []storage.Document
	chunks      map[int64][]storage.Chunk
	nextChunkID int64
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

func (s *memoryStrategyStore) ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	if s.chunks == nil {
		s.chunks = map[int64][]storage.Chunk{}
	}

	removed := map[int64]struct{}{}
	for _, chunkID := range plan.RemoveIDs {
		removed[chunkID] = struct{}{}
	}

	current := s.chunks[documentID]
	var kept []storage.Chunk
	for _, chunk := range current {
		if _, ok := removed[chunk.ID]; !ok {
			kept = append(kept, chunk)
		}
	}

	for _, chunk := range plan.Keep {
		for i := range kept {
			if kept[i].ID == chunk.ID {
				kept[i] = chunk
				break
			}
		}
	}

	inserted := make([]storage.Chunk, len(plan.Insert))
	for i, chunk := range plan.Insert {
		if s.nextChunkID == 0 {
			s.nextChunkID = 1
		}
		chunk.ID = s.nextChunkID
		chunk.DocumentID = documentID
		s.nextChunkID++
		inserted[i] = chunk
		kept = append(kept, chunk)
	}

	s.chunks[documentID] = kept
	return inserted, nil
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

func (s *memoryStrategyStore) MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error {
	for i := range s.documents {
		if s.documents[i].FileID != fileID {
			continue
		}
		s.documents[i].Status = storage.DocumentStatusEmbedded
		s.documents[i].EmbeddedContentHash = contentHash
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
