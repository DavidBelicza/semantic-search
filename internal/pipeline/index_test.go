package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidbelicza/semantic-search/core/search"
	"github.com/davidbelicza/semantic-search/core/storage"
	"github.com/davidbelicza/semantic-search/core/storage/sqlite"
	"github.com/davidbelicza/semantic-search/core/storage/sqlitevec"
	"github.com/davidbelicza/semantic-search/core/strategy"
	"github.com/davidbelicza/semantic-search/core/strategy/general"
	"github.com/davidbelicza/semantic-search/core/strategy/markdown"
	"github.com/davidbelicza/semantic-search/internal/pipeline"
)

func TestIndexDiscoversRegistersAndFingerprints(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	nested := filepath.Join(root, "notes")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, file := range []string{
		filepath.Join(root, "README.md"),
		filepath.Join(nested, "plan.md"),
		filepath.Join(root, "ignore.txt"),
	} {
		if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
			t.Fatalf("write %q: %v", file, err)
		}
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}

	pool := strategy.NewPool(markdown.NewMarkdownStrategy(general.NewGeneralStrategy(nil, nil)))
	if err := pipeline.Index(context.Background(), store, pool, root, pipeline.Options{}, false, nil); err != nil {
		t.Fatalf("index: %v", err)
	}

	docs, err := store.DocumentsByStatus(context.Background(), storage.DocumentStatusScanned, 0, 100)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 scanned markdown docs (the .txt is unclaimed), got %d", len(docs))
	}
}

// stubModel and stubEmbedder let the pipeline run end to end without a real embedding server.
type stubModel struct{ dim int }

func (m stubModel) Name() string                         { return "stub" }
func (m stubModel) Dimensions() int                      { return m.dim }
func (stubModel) BuildData(c storage.Chunk) string       { return c.Text }
func (stubModel) BuildQuery(q, _ string) (string, error) { return q, nil }

type stubEmbedder struct{ dim int }

func (e stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func TestPipelineIndexProcessSearchCleanup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	remove := filepath.Join(root, "remove.txt")
	if err := os.WriteFile(keep, []byte("The vacation policy grants fifteen paid days."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remove, []byte("The office closes on public holidays."), 0o644); err != nil {
		t.Fatal(err)
	}

	dbDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dbDir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	vectors, err := sqlitevec.Open(ctx, filepath.Join(dbDir, "vectors.db"), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer vectors.Close()

	model := stubModel{dim: 8}
	embedder := stubEmbedder{dim: 8}
	pool := strategy.NewPool(general.NewGeneralStrategy(model, embedder))

	if err := pipeline.Index(ctx, store, pool, root, pipeline.Options{}, false, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := pipeline.Process(ctx, store, vectors, pool, false, nil); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Touch a file (new mtime, same content) then re-index: the document returns to "indexed",
	// and fingerprinting restores it to "embedded" because its content hash is unchanged.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(keep, future, future); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Index(ctx, store, pool, root, pipeline.Options{}, false, nil); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	searcher := pipeline.NewDocumentSearcher(store, vectors, model, embedder)
	docs, err := searcher.Search(ctx, search.SearchConfig{Query: "vacation"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(docs) == 0 || docs[0].FileName == "" || len(docs[0].Chunks) == 0 {
		t.Fatalf("expected hydrated document results, got %#v", docs)
	}

	if err := os.Remove(remove); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Cleanup(ctx, store, vectors, false, nil); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	remaining, err := store.DocumentsFromID(ctx, 0, 100)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("expected one document after cleanup, got %v %+v", err, remaining)
	}
}

func TestIndexFollowsSymlinksAndSkipsHidden(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	hidden := filepath.Join(root, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "secret.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}

	pool := strategy.NewPool(markdown.NewMarkdownStrategy(general.NewGeneralStrategy(nil, nil)))
	if err := pipeline.Index(ctx, store, pool, root, pipeline.Options{FollowSymlinks: true}, false, nil); err != nil {
		t.Fatalf("index: %v", err)
	}

	docs, err := store.DocumentsByStatus(ctx, storage.DocumentStatusScanned, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected at least the real markdown file indexed")
	}
	for _, d := range docs {
		if strings.Contains(d.AbsolutePath, ".hidden") {
			t.Fatalf("hidden file should be skipped: %q", d.AbsolutePath)
		}
	}
}

// fakeIndexStore is a configurable IndexStore: each error field, when set, makes the matching
// method fail, and toFingerprint is served once by DocumentsByStatus so the fingerprint loop
// terminates.
type fakeIndexStore struct {
	upsertErr     error
	byStatusErr   error
	updateHashErr error
	checkpointErr error
	toFingerprint []storage.Document
	served        bool
}

func (s *fakeIndexStore) UpsertDocuments(context.Context, []storage.FileMetadata) error {
	return s.upsertErr
}

func (s *fakeIndexStore) DocumentsByStatus(_ context.Context, _ string, _ int64, _ int) ([]storage.Document, error) {
	if s.byStatusErr != nil {
		return nil, s.byStatusErr
	}
	if s.served {
		return nil, nil
	}
	s.served = true
	return s.toFingerprint, nil
}

func (s *fakeIndexStore) UpdateDocumentContentHashAndStatus(context.Context, string, string, string) error {
	return s.updateHashErr
}

func (s *fakeIndexStore) UpdateDocumentScanCheckpointAndStatus(context.Context, string, string) error {
	return s.checkpointErr
}

// probeStrategy is a configurable strategy for the index walk: it can claim by extension, fail
// CreateMetadata, and return a fixed fingerprint.
type probeStrategy struct {
	claimExt string
	metaErr  error
	fp       string
}

func (s probeStrategy) Claims(path string) bool {
	return s.claimExt == "" || strings.HasSuffix(path, s.claimExt)
}
func (probeStrategy) Parse([]byte) (strategy.ParsedDocument, error) {
	return strategy.ParsedDocument{}, nil
}
func (probeStrategy) Chunk(storage.Document, strategy.ParsedDocument) ([]storage.Chunk, error) {
	return nil, nil
}
func (probeStrategy) Embed(context.Context, []storage.Chunk) ([][]float32, error) { return nil, nil }
func (s probeStrategy) Fingerprint([]byte) string                                 { return s.fp }
func (s probeStrategy) CreateMetadata(ref strategy.FileRef) (storage.FileMetadata, error) {
	if s.metaErr != nil {
		return storage.FileMetadata{}, s.metaErr
	}
	return storage.FileMetadata{FileID: ref.Path, AbsolutePath: ref.Path}, nil
}

func TestIndexReturnsDiscoverError(t *testing.T) {
	pool := strategy.NewPool(probeStrategy{})
	err := pipeline.Index(context.Background(), &fakeIndexStore{}, pool, filepath.Join(t.TempDir(), "missing"), pipeline.Options{}, false, nil)
	if err == nil {
		t.Fatal("expected a discover error for a nonexistent root")
	}
}

func TestIndexReturnsUpsertError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pool := strategy.NewPool(probeStrategy{})
	store := &fakeIndexStore{upsertErr: errors.New("upsert failed")}
	if err := pipeline.Index(context.Background(), store, pool, dir, pipeline.Options{}, false, nil); err == nil {
		t.Fatal("expected an upsert error")
	}
}

func TestDiscoverSkipsHiddenFilesAndUnfollowedSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".secret.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real.md")
	if err := os.WriteFile(real, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	pool := strategy.NewPool(probeStrategy{})
	store := &fakeIndexStore{}
	// FollowSymlinks is false, so the symlink is stat'd as non-regular and skipped; the hidden
	// file is skipped too. Only real.md is a regular claimed file.
	if err := pipeline.Index(context.Background(), store, pool, dir, pipeline.Options{}, false, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
}

func TestFileMetadataErrorsAbortDiscovery(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pool := strategy.NewPool(probeStrategy{metaErr: errors.New("metadata failed")})
	if err := pipeline.Index(context.Background(), &fakeIndexStore{}, pool, dir, pipeline.Options{}, false, nil); err == nil {
		t.Fatal("expected a CreateMetadata error to abort discovery")
	}
}

func TestFileInfoErrorOnFollowedDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "nowhere.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	pool := strategy.NewPool(probeStrategy{})
	if err := pipeline.Index(context.Background(), &fakeIndexStore{}, pool, dir, pipeline.Options{FollowSymlinks: true}, false, nil); err == nil {
		t.Fatal("expected a stat error following a dangling symlink")
	}
}

func TestFingerprintReturnsByStatusError(t *testing.T) {
	pool := strategy.NewPool(probeStrategy{})
	store := &fakeIndexStore{byStatusErr: errors.New("by status failed")}
	if err := pipeline.Index(context.Background(), store, pool, t.TempDir(), pipeline.Options{}, false, nil); err == nil {
		t.Fatal("expected a DocumentsByStatus error")
	}
}

func TestFingerprintNoStrategyForDocument(t *testing.T) {
	store := &fakeIndexStore{toFingerprint: []storage.Document{
		{ID: 1, FileID: "1", AbsolutePath: "/gone/file.md"},
	}}
	pool := strategy.NewPool() // empty: no strategy claims the document
	if err := pipeline.Index(context.Background(), store, pool, t.TempDir(), pipeline.Options{}, true, nil); err == nil {
		t.Fatal("expected a no-strategy error")
	}
}

func TestFingerprintDocumentBranches(t *testing.T) {
	dir := t.TempDir()
	matchFile := filepath.Join(dir, "match.md")
	if err := os.WriteFile(matchFile, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &fakeIndexStore{toFingerprint: []storage.Document{
		// Metadata matches the scanned checkpoint: no hashing, just a checkpoint advance.
		{ID: 1, FileID: "1", AbsolutePath: matchFile, HasHash: true, HasScannedMetadata: true,
			FileSize: 10, ScannedFileSize: 10, ModifiedAtNS: 5, ScannedModifiedAtNS: 5},
		// Content hash unchanged from what was scanned: restore to scanned via checkpoint.
		{ID: 2, FileID: "2", AbsolutePath: matchFile, HasHash: true, ContentHash: "H"},
		// File is gone: the read fails and the error is collected.
		{ID: 3, FileID: "3", AbsolutePath: filepath.Join(dir, "gone.md")},
	}}
	pool := strategy.NewPool(probeStrategy{fp: "H"})

	// failFast false collects the missing-file error but still processes the others.
	if err := pipeline.Index(context.Background(), store, pool, dir, pipeline.Options{}, false, nil); err == nil {
		t.Fatal("expected the collected read error")
	}
}

func TestFingerprintFailFastStopsOnFirstError(t *testing.T) {
	store := &fakeIndexStore{toFingerprint: []storage.Document{
		{ID: 1, FileID: "1", AbsolutePath: "/gone/file.md"},
	}}
	pool := strategy.NewPool(probeStrategy{fp: "H"})
	if err := pipeline.Index(context.Background(), store, pool, t.TempDir(), pipeline.Options{}, true, nil); err == nil {
		t.Fatal("expected a fail-fast read error")
	}
}

func TestFingerprintUpdateHashError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "changed.md")
	if err := os.WriteFile(file, []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeIndexStore{
		updateHashErr: errors.New("update failed"),
		toFingerprint: []storage.Document{{ID: 1, FileID: "1", AbsolutePath: file, ContentHash: "old"}},
	}
	pool := strategy.NewPool(probeStrategy{fp: "new"})
	if err := pipeline.Index(context.Background(), store, pool, dir, pipeline.Options{}, true, nil); err == nil {
		t.Fatal("expected an update-content-hash error")
	}
}

func TestMarkCheckpointError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "m.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeIndexStore{
		checkpointErr: errors.New("checkpoint failed"),
		toFingerprint: []storage.Document{{ID: 1, FileID: "1", AbsolutePath: file,
			HasHash: true, HasScannedMetadata: true, FileSize: 1, ScannedFileSize: 1,
			ModifiedAtNS: 2, ScannedModifiedAtNS: 2}},
	}
	pool := strategy.NewPool(probeStrategy{fp: "H"})
	if err := pipeline.Index(context.Background(), store, pool, dir, pipeline.Options{}, true, nil); err == nil {
		t.Fatal("expected a checkpoint error")
	}
}
