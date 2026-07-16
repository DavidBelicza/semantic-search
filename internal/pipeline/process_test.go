package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/strategy"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestProcessScannedReconcilesEmbedsAndMarks(t *testing.T) {
	path := writeFile(t, "abcdefg")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{42: {{ID: 99, DocumentID: 42, ChunkIndex: 0, Text: "old", ContentHash: "old"}}},
		nextID:    100,
	}
	vectorStore := &memoryVectorStore{}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, vectorStore, pool, false, nil); err != nil {
		t.Fatalf("process: %v", err)
	}

	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: %q", store.documents[0].Status)
	}
	if len(store.chunks[42]) != 3 {
		t.Fatalf("stored chunk count mismatch: %d", len(store.chunks[42]))
	}
	if len(vectorStore.deleted) != 1 || vectorStore.deleted[0] != 99 {
		t.Fatalf("deleted vectors mismatch: %#v", vectorStore.deleted)
	}
	if len(vectorStore.embeddings) != 3 {
		t.Fatalf("embedding count mismatch: %d", len(vectorStore.embeddings))
	}
}

func TestProcessScannedLeavesChunkedWhenEmbeddingFails(t *testing.T) {
	path := writeFile(t, "abc")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		nextID:    100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3, embedErr: errors.New("boom")})

	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false, nil); err == nil {
		t.Fatal("expected embedding error")
	}
	if store.documents[0].Status != storage.DocumentStatusChunked {
		t.Fatalf("status mismatch: want chunked, got %q", store.documents[0].Status)
	}
}

func TestProcessChunkedEmbedsAndMarks(t *testing.T) {
	path := writeFile(t, "abc")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusChunked}},
		chunks: map[int64][]storage.Chunk{42: {
			{ID: 100, DocumentID: 42, ChunkIndex: 0, Text: "abc", ContentHash: "h1"},
			{ID: 101, DocumentID: 42, ChunkIndex: 1, Text: "def", ContentHash: "h2"},
		}},
	}
	vectorStore := &memoryVectorStore{}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, vectorStore, pool, false, nil); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(vectorStore.embeddings) != 2 {
		t.Fatalf("embedding count mismatch: %d", len(vectorStore.embeddings))
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("status mismatch: %q", store.documents[0].Status)
	}
}

func TestProcessScannedContinuesAfterErrorWhenNotFailFast(t *testing.T) {
	good := writeFile(t, "abcdef")
	store := &memoryStore{
		documents: []storage.Document{
			{ID: 1, FileID: "1:1", AbsolutePath: "/does/not/exist.md", Status: storage.DocumentStatusScanned},
			{ID: 2, FileID: "1:2", AbsolutePath: good, Status: storage.DocumentStatusScanned},
		},
		nextID: 100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false, nil); err == nil {
		t.Fatal("expected an aggregated error for the missing file")
	}
	if store.documents[1].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("valid document was not processed: %q", store.documents[1].Status)
	}
}

// --- test doubles ---

type fakeStrategy struct {
	maxRunes int
	embedErr error
}

func (fakeStrategy) Claims(string) bool { return true }
func (fakeStrategy) CreateMetadata(strategy.FileRef) (storage.FileMetadata, error) {
	return storage.FileMetadata{}, nil
}
func (fakeStrategy) Fingerprint([]byte) string { return "" }
func (fakeStrategy) Parse(content []byte) (strategy.ParsedDocument, error) {
	return strategy.ParsedDocument{Sections: []strategy.Section{{Body: string(content)}}}, nil
}

func (s fakeStrategy) Chunk(_ storage.Document, parsed strategy.ParsedDocument) ([]storage.Chunk, error) {
	text := ""
	if len(parsed.Sections) > 0 {
		text = parsed.Sections[0].Body
	}
	runes := []rune(text)
	var chunks []storage.Chunk
	for start := 0; start < len(runes); start += s.maxRunes {
		end := start + s.maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		piece := string(runes[start:end])
		sum := sha256.Sum256([]byte(piece))
		chunks = append(chunks, storage.Chunk{ChunkIndex: len(chunks), Text: piece, ContentHash: hex.EncodeToString(sum[:])})
	}
	return chunks, nil
}

func (s fakeStrategy) Embed(_ context.Context, chunks []storage.Chunk) ([][]float32, error) {
	if s.embedErr != nil {
		return nil, s.embedErr
	}
	vectors := make([][]float32, len(chunks))
	for i := range chunks {
		vectors[i] = []float32{float32(i)}
	}
	return vectors, nil
}

type memoryStore struct {
	documents []storage.Document
	chunks    map[int64][]storage.Chunk
	nextID    int64
}

func (s *memoryStore) DocumentsByStatus(_ context.Context, status string, afterID int64, limit int) ([]storage.Document, error) {
	var out []storage.Document
	for _, doc := range s.documents {
		if doc.Status != status || doc.ID <= afterID {
			continue
		}
		out = append(out, doc)
		if len(out) == limit {
			return out, nil
		}
	}
	return out, nil
}

func (s *memoryStore) ApplyDocumentChunkReconcile(_ context.Context, documentID int64, plan storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	if s.chunks == nil {
		s.chunks = map[int64][]storage.Chunk{}
	}
	removed := map[int64]struct{}{}
	for _, id := range plan.RemoveIDs {
		removed[id] = struct{}{}
	}
	var kept []storage.Chunk
	for _, chunk := range s.chunks[documentID] {
		if _, ok := removed[chunk.ID]; !ok {
			kept = append(kept, chunk)
		}
	}
	inserted := make([]storage.Chunk, len(plan.Insert))
	for i, chunk := range plan.Insert {
		if s.nextID == 0 {
			s.nextID = 1
		}
		chunk.ID = s.nextID
		chunk.DocumentID = documentID
		s.nextID++
		inserted[i] = chunk
		kept = append(kept, chunk)
	}
	s.chunks[documentID] = kept
	return inserted, nil
}

func (s *memoryStore) ChunksByDocumentID(_ context.Context, documentID int64) ([]storage.Chunk, error) {
	return s.chunks[documentID], nil
}

func (s *memoryStore) UpdateDocumentStatus(_ context.Context, fileID string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].Status = status
		}
	}
	return nil
}

func (s *memoryStore) MarkDocumentEmbedded(_ context.Context, fileID string, contentHash string) error {
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

func (s *memoryVectorStore) Delete(_ context.Context, chunkIDs []int64) error {
	s.deleted = append(s.deleted, chunkIDs...)
	return nil
}

func (s *memoryVectorStore) Replace(_ context.Context, embeddings []storage.ChunkEmbedding) error {
	s.embeddings = append(s.embeddings, embeddings...)
	return nil
}

type replaceErrVectorStore struct{ memoryVectorStore }

func (*replaceErrVectorStore) Replace(context.Context, []storage.ChunkEmbedding) error {
	return errors.New("replace failed")
}

type markErrStore struct{ *memoryStore }

func (markErrStore) MarkDocumentEmbedded(context.Context, string, string) error {
	return errors.New("mark failed")
}

func TestProcessScannedReturnsVectorReplaceError(t *testing.T) {
	path := writeFile(t, "abcdefg")
	store := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{},
		nextID:    100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), store, &replaceErrVectorStore{}, pool, false, nil); err == nil {
		t.Fatal("expected a vector replace error")
	}
}

func TestProcessScannedReturnsMarkEmbeddedError(t *testing.T) {
	path := writeFile(t, "abcdefg")
	inner := &memoryStore{
		documents: []storage.Document{{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}},
		chunks:    map[int64][]storage.Chunk{},
		nextID:    100,
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})

	if err := Process(context.Background(), markErrStore{inner}, &memoryVectorStore{}, pool, false, nil); err == nil {
		t.Fatal("expected a mark-embedded error")
	}
}

// --- erroring stores (embed *memoryStore and override one method) ---

type byStatusErrStore struct{ *memoryStore }

func (byStatusErrStore) DocumentsByStatus(context.Context, string, int64, int) ([]storage.Document, error) {
	return nil, errors.New("by status failed")
}

type reconcileErrStore struct{ *memoryStore }

func (reconcileErrStore) ApplyDocumentChunkReconcile(context.Context, int64, storage.ChunkReconcilePlan) ([]storage.Chunk, error) {
	return nil, errors.New("reconcile failed")
}

type chunksErrStore struct{ *memoryStore }

func (chunksErrStore) ChunksByDocumentID(context.Context, int64) ([]storage.Chunk, error) {
	return nil, errors.New("chunks failed")
}

type updateStatusErrStore struct{ *memoryStore }

func (updateStatusErrStore) UpdateDocumentStatus(context.Context, string, string) error {
	return errors.New("update status failed")
}

// secondChunksErrStore serves the first ChunksByDocumentID call (used by processScanned to load
// existing chunks) and fails the second (used by chunksForEmbedding), isolating that branch.
type secondChunksErrStore struct {
	*memoryStore
	calls int
}

func (s *secondChunksErrStore) ChunksByDocumentID(ctx context.Context, id int64) ([]storage.Chunk, error) {
	s.calls++
	if s.calls >= 2 {
		return nil, errors.New("second chunks lookup failed")
	}
	return s.memoryStore.ChunksByDocumentID(ctx, id)
}

type deleteErrVectorStore struct{ memoryVectorStore }

func (*deleteErrVectorStore) Delete(context.Context, []int64) error {
	return errors.New("delete failed")
}

// --- strategy variants ---

type parseErrStrategy struct{ fakeStrategy }

func (parseErrStrategy) Parse([]byte) (strategy.ParsedDocument, error) {
	return strategy.ParsedDocument{}, errors.New("parse failed")
}

type countMismatchStrategy struct{ fakeStrategy }

func (countMismatchStrategy) Embed(context.Context, []storage.Chunk) ([][]float32, error) {
	return [][]float32{}, nil // fewer vectors than chunks
}

type raggedStrategy struct{ fakeStrategy }

func (raggedStrategy) Embed(_ context.Context, chunks []storage.Chunk) ([][]float32, error) {
	out := make([][]float32, len(chunks))
	for i := range chunks {
		out[i] = []float32{1}
	}
	if len(out) > 1 {
		out[1] = []float32{} // second vector has a different dimension
	}
	return out, nil
}

// --- tests ---

func scannedDoc(path string) storage.Document {
	return storage.Document{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusScanned}
}

func TestProcessByStatusError(t *testing.T) {
	store := byStatusErrStore{&memoryStore{}}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false, nil); err == nil {
		t.Fatal("expected a DocumentsByStatus error")
	}
}

func TestProcessScannedFailFastReturnsError(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc("/does/not/exist.md")}, nextID: 100}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a fail-fast read error")
	}
}

func TestProcessScannedNoStrategy(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "x"))}, nextID: 100}
	if err := Process(context.Background(), store, &memoryVectorStore{}, strategy.NewPool(), true, nil); err == nil {
		t.Fatal("expected a no-strategy error")
	}
}

func TestProcessScannedParseError(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "x"))}, nextID: 100}
	pool := strategy.NewPool(parseErrStrategy{})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestProcessScannedLoadExistingChunksError(t *testing.T) {
	store := chunksErrStore{&memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abc"))}, nextID: 100}}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a load-existing-chunks error")
	}
}

func TestProcessScannedReconcileError(t *testing.T) {
	store := reconcileErrStore{&memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abc"))}, nextID: 100}}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a reconcile error")
	}
}

func TestProcessScannedDeleteVectorsError(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abc"))}, chunks: map[int64][]storage.Chunk{}, nextID: 100}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &deleteErrVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a delete-vectors error")
	}
}

func TestProcessScannedUpdateStatusError(t *testing.T) {
	store := updateStatusErrStore{&memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abc"))}, chunks: map[int64][]storage.Chunk{}, nextID: 100}}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected an update-status error")
	}
}

func TestProcessScannedChunksForEmbeddingError(t *testing.T) {
	// A never-embedded document with existing chunks re-loads chunks in chunksForEmbedding; make
	// that second lookup fail.
	inner := &memoryStore{
		documents: []storage.Document{scannedDoc(writeFile(t, "abcdef"))},
		chunks:    map[int64][]storage.Chunk{42: {{ID: 7, DocumentID: 42, ChunkIndex: 0, Text: "old", ContentHash: "old"}}},
		nextID:    100,
	}
	store := &secondChunksErrStore{memoryStore: inner}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a chunks-for-embedding error")
	}
}

func TestProcessScannedAlreadyEmbeddedReindex(t *testing.T) {
	// EmbeddedContentHash set: chunksForEmbedding returns only the newly inserted chunks.
	doc := scannedDoc(writeFile(t, "abcdef"))
	doc.EmbeddedContentHash = "prev"
	store := &memoryStore{documents: []storage.Document{doc}, chunks: map[int64][]storage.Chunk{}, nextID: 100}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false, nil); err != nil {
		t.Fatalf("process: %v", err)
	}
}

func TestProcessScannedEmptyFileEmbedsNothing(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, ""))}, chunks: map[int64][]storage.Chunk{}, nextID: 100}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, false, nil); err != nil {
		t.Fatalf("process: %v", err)
	}
	if store.documents[0].Status != storage.DocumentStatusEmbedded {
		t.Fatalf("expected embedded status, got %q", store.documents[0].Status)
	}
}

func TestProcessEmbedCountMismatch(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abc"))}, chunks: map[int64][]storage.Chunk{}, nextID: 100}
	pool := strategy.NewPool(countMismatchStrategy{fakeStrategy{maxRunes: 3}})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected an embedding-count mismatch error")
	}
}

func TestProcessEmbedDimensionMismatch(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{scannedDoc(writeFile(t, "abcdef"))}, chunks: map[int64][]storage.Chunk{}, nextID: 100}
	pool := strategy.NewPool(raggedStrategy{fakeStrategy{maxRunes: 3}})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected an embedding-dimension mismatch error")
	}
}

func chunkedDoc(path string) storage.Document {
	return storage.Document{ID: 42, FileID: "1:100", AbsolutePath: path, Status: storage.DocumentStatusChunked}
}

func TestProcessChunkedNoStrategy(t *testing.T) {
	store := &memoryStore{documents: []storage.Document{chunkedDoc(writeFile(t, "abc"))}}
	if err := Process(context.Background(), store, &memoryVectorStore{}, strategy.NewPool(), true, nil); err == nil {
		t.Fatal("expected a no-strategy error for the chunked pass")
	}
}

func TestProcessChunkedLoadChunksError(t *testing.T) {
	store := chunksErrStore{&memoryStore{documents: []storage.Document{chunkedDoc(writeFile(t, "abc"))}}}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected a load-chunks error for the chunked pass")
	}
}

func TestProcessChunkedEmbedError(t *testing.T) {
	store := &memoryStore{
		documents: []storage.Document{chunkedDoc(writeFile(t, "abc"))},
		chunks:    map[int64][]storage.Chunk{42: {{ID: 1, DocumentID: 42, ChunkIndex: 0, Text: "abc"}}},
	}
	pool := strategy.NewPool(fakeStrategy{maxRunes: 3, embedErr: errors.New("embed boom")})
	if err := Process(context.Background(), store, &memoryVectorStore{}, pool, true, nil); err == nil {
		t.Fatal("expected an embed error for the chunked pass")
	}
}

// Every other test fits inside a single page.
func TestProcessSpansMultiplePages(t *testing.T) {
	const documents = processBatchSize*2 + 1

	dir := t.TempDir()
	store := &memoryStore{chunks: map[int64][]storage.Chunk{}, nextID: 1000}
	for i := range documents {
		path := filepath.Join(dir, fmt.Sprintf("note%d.md", i))
		if err := os.WriteFile(path, []byte("abcdefg"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		store.documents = append(store.documents, storage.Document{
			ID:           int64(i + 1),
			FileID:       fmt.Sprintf("1:%d", i),
			AbsolutePath: path,
			Status:       storage.DocumentStatusScanned,
		})
	}

	var calls []progressCall
	tracker := NewProgressTracker(recorder(&calls))
	tracker.Start(PhaseIndexing, documents)

	if err := Process(context.Background(), store, &memoryVectorStore{}, strategy.NewPool(fakeStrategy{maxRunes: 3}), true, tracker); err != nil {
		t.Fatalf("process: %v", err)
	}

	for i, document := range store.documents {
		if document.Status != storage.DocumentStatusEmbedded {
			t.Fatalf("document %d: status %q, want embedded", i, document.Status)
		}
	}
	last := calls[len(calls)-1]
	if last != (progressCall{PhaseIndexing, documents, documents}) {
		t.Fatalf("after process: got %+v, want {indexing %d %d}", last, documents, documents)
	}
}
