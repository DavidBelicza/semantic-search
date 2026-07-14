package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	if err := pipeline.Index(context.Background(), store, pool, root, pipeline.Options{}, false); err != nil {
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

	if err := pipeline.Index(ctx, store, pool, root, pipeline.Options{}, false); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := pipeline.Process(ctx, store, vectors, pool, false); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Re-index unchanged files: hits the fingerprint checkpoint fast path.
	if err := pipeline.Index(ctx, store, pool, root, pipeline.Options{}, false); err != nil {
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
	if err := pipeline.Cleanup(ctx, store, vectors, false); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	remaining, err := store.DocumentsFromID(ctx, 0, 100)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("expected one document after cleanup, got %v %+v", err, remaining)
	}
}
