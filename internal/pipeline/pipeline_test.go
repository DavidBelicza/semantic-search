package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"semantic-search/internal/ingest"
	storage "semantic-search/internal/storage/sqlite"
	"semantic-search/internal/strategy"
)

func markdownPool(text string) strategy.Pool {
	return strategy.NewPool(fakeStrategy{text: text, maxRunes: 3})
}

func TestProcessScannedReconcilesChunksEmbedsAndMarksEmbedded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned},
		},
		chunks: map[int64][]storage.Chunk{
			42: {{ID: 99, DocumentID: 42, ChunkIndex: 0, Text: "old", ContentHash: "old"}},
		},
		nextChunkID: 100,
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, markdownPool("abcdefg"), fakeEmbedder{}).processScanned(context.Background(), false)
	if err != nil {
		t.Fatalf("process scanned: %v", err)
	}

	if result.Processed != 1 || result.Embedded != 1 {
		t.Fatalf("result mismatch: %#v", result)
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

func TestProcessScannedKeepsUnchangedChunksAndEmbedsNewChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned, ContentHash: "new-content-hash", EmbeddedContentHash: "old-content-hash"},
		},
		chunks: map[int64][]storage.Chunk{
			42: {{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"}},
		},
		nextChunkID: 101,
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, markdownPool("abcdef"), fakeEmbedder{}).processScanned(context.Background(), false)
	if err != nil {
		t.Fatalf("process scanned: %v", err)
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

func TestProcessScannedEmbedsAllChunksWhenNeverEmbedded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "cb8379ac2098aa165029e3938a51da0bcecfc008fd6795f401178647f96c5b34"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, markdownPool("abcdef"), fakeEmbedder{}).processScanned(context.Background(), false)
	if err != nil {
		t.Fatalf("process scanned: %v", err)
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

func TestProcessScannedSkipsReembeddingWhenAlreadyEmbeddedAndUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned, ContentHash: "content-hash", EmbeddedContentHash: "content-hash"},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "cb8379ac2098aa165029e3938a51da0bcecfc008fd6795f401178647f96c5b34"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, markdownPool("abcdef"), fakeEmbedder{}).processScanned(context.Background(), false)
	if err != nil {
		t.Fatalf("process scanned: %v", err)
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

func TestProcessChunkedEmbedsAndMarksEmbedded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusChunked},
		},
		chunks: map[int64][]storage.Chunk{
			42: {
				{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "hash-1"},
				{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "hash-2"},
			},
		},
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, strategy.NewPool(), fakeEmbedder{}).processChunked(context.Background(), false)
	if err != nil {
		t.Fatalf("process chunked: %v", err)
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

func TestProcessScannedLeavesDocumentChunkedWhenEmbeddingFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned},
		},
		nextChunkID: 100,
	}

	_, err := newProcessingPipeline(store, &memoryVectorStore{}, markdownPool("abc"), fakeFailingEmbedder{}).processScanned(context.Background(), false)
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

func TestProcessScannedContinuesAfterErrorWhenNotFailFast(t *testing.T) {
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 1, FileID: "1:1", AbsolutePath: "/x/unsupported.txt", Status: storage.DocumentStatusScanned},
			{ID: 2, FileID: "1:2", AbsolutePath: "/x/note.md", Status: storage.DocumentStatusScanned},
		},
		nextChunkID: 100,
	}
	vectorStore := &memoryVectorStore{}

	result, err := newProcessingPipeline(store, vectorStore, markdownPool("abc"), fakeEmbedder{}).processScanned(context.Background(), false)
	if err == nil {
		t.Fatal("expected an aggregated error for the unsupported document")
	}
	if result.Processed != 1 || result.Embedded != 1 {
		t.Fatalf("expected the supported document to still process: %#v", result)
	}
	if store.documents[1].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("supported document was not embedded: %q", store.documents[1].Status)
	}
}

func TestProcessScannedStopsOnFirstErrorWhenFailFast(t *testing.T) {
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 1, FileID: "1:1", AbsolutePath: "/x/unsupported.txt", Status: storage.DocumentStatusScanned},
			{ID: 2, FileID: "1:2", AbsolutePath: "/x/note.md", Status: storage.DocumentStatusScanned},
		},
		nextChunkID: 100,
	}

	result, err := newProcessingPipeline(store, &memoryVectorStore{}, markdownPool("abc"), fakeEmbedder{}).processScanned(context.Background(), true)
	if err == nil {
		t.Fatal("expected an error")
	}
	if result.Processed != 0 {
		t.Fatalf("expected abort before the supported document: %#v", result)
	}
	if store.documents[1].Status == storage.DocumentStatusEmbedded {
		t.Fatal("supported document must not be processed in fail-fast mode")
	}
}

// --- test doubles ---

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
	runes := []rune(text)
	var chunks []storage.Chunk
	for start := 0; start < len(runes); start += s.maxRunes {
		end := start + s.maxRunes
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

	return chunks, nil
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

type memoryStore struct {
	documents   []storage.Document
	chunks      map[int64][]storage.Chunk
	nextChunkID int64
}

func (s *memoryStore) DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
	var documents []storage.Document
	for _, document := range s.documents {
		if document.Status != status || document.ID <= afterID {
			continue
		}
		documents = append(documents, document)
		if len(documents) == limit {
			return documents, nil
		}
	}

	return documents, nil
}

func (s *memoryStore) ApplyDocumentChunkReconcile(ctx context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
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

func (s *memoryStore) ChunksByDocumentID(ctx context.Context, documentID int64) ([]storage.Chunk, error) {
	return s.chunks[documentID], nil
}

func (s *memoryStore) UpdateDocumentStatus(ctx context.Context, fileID string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].Status = status
		}
	}

	return nil
}

func (s *memoryStore) MarkDocumentEmbedded(ctx context.Context, fileID string, contentHash string) error {
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
