package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/davidbelicza/semantic-search"
	"github.com/davidbelicza/semantic-search/core/storage"
)

// TestEndToEnd is an example of the full composition: build an engine from an embedder, two
// stores, and a set of strategies, index a directory of mixed file types, then search it.
func TestEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeFixtures(t, dir)

	const dimensions = 1024

	store, err := semanticsearch.NewSQLiteStorage(ctx, filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	vectors, err := semanticsearch.NewSQLiteVectorStorage(ctx, filepath.Join(dir, "vectors.db"), dimensions)
	if err != nil {
		t.Fatalf("open vector storage: %v", err)
	}
	defer vectors.Close()

	engine := newEngine(t, store, vectors, dimensions)
	assertRetrieval(t, engine, dir)
}

// TestEndToEndPostgres runs the same composition against PostgreSQL + pgvector. It is skipped
// unless SEMANTIC_SEARCH_POSTGRES_DSN is set (e.g. the docker/docker-compose.yml database).
func TestEndToEndPostgres(t *testing.T) {
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run the postgres end-to-end test")
	}

	ctx := context.Background()
	dir := t.TempDir()
	writeFixtures(t, dir)
	resetPostgres(t, dsn)

	const dimensions = 1024

	store, err := semanticsearch.NewPostgresStorage(ctx, dsn)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	vectors, err := semanticsearch.NewPostgresVectorStorage(ctx, dsn, dimensions, semanticsearch.PostgresKNN)
	if err != nil {
		t.Fatalf("open vector storage: %v", err)
	}
	defer vectors.Close()

	engine := newEngine(t, store, vectors, dimensions)
	assertRetrieval(t, engine, dir)
}

func newEngine(t *testing.T, store storage.Storage, vectors storage.VectorStorage, dimensions int) *semanticsearch.Engine {
	t.Helper()
	engine, err := semanticsearch.NewEngine(semanticsearch.Config{
		Model:         plainModel{dim: dimensions},      // production: semanticsearch.NewModel(...)
		Embedder:      hashingEmbedder{dim: dimensions}, // production: semanticsearch.NewAiEmbedder(...)
		Storage:       store,
		VectorStorage: vectors,
		Strategies: []semanticsearch.StrategyFactory{
			semanticsearch.NewTextStrategy(),
			semanticsearch.NewMarkdownStrategy(),
			semanticsearch.NewCodeStrategy(),
			semanticsearch.NewDocxStrategy(),
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	return engine
}

// assertRetrieval indexes the fixtures and checks that each query's top result comes from the
// expected file, across all four formats.
func assertRetrieval(t *testing.T, engine *semanticsearch.Engine, dir string) {
	t.Helper()
	ctx := context.Background()

	if err := engine.Index(ctx, dir, semanticsearch.IndexOptions{}); err != nil {
		t.Fatalf("index: %v", err)
	}

	cases := []struct {
		query string
		want  string // a distinctive word expected in the top result
	}{
		{"how many paid vacation days do employees get", "vacation"}, // → vacation.txt
		{"refund to the original payment method", "refund"},          // → billing.md
		{"read the configuration file from disk", "config"},          // → loader.go
		{"working remotely from home policy", "remote"},              // → handbook.docx
	}

	for _, tc := range cases {
		results, err := engine.Search(ctx, semanticsearch.SearchConfig{Query: tc.query})
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		if len(results) == 0 || len(results[0].Chunks) == 0 {
			t.Fatalf("query %q: no results", tc.query)
		}
		top := results[0].Chunks[0]
		if !strings.Contains(strings.ToLower(top.Text), tc.want) {
			t.Errorf("query %q: want top result containing %q, got title=%q text=%q", tc.query, tc.want, top.Title, top.Text)
		}
	}
}

// resetPostgres drops the tables the stores use so each run starts clean.
func resetPostgres(t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open reset connection: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE IF EXISTS chunks, documents, chunk_vectors CASCADE"); err != nil {
		t.Fatalf("reset postgres: %v", err)
	}
}

// plainModel is a neutral model for the end-to-end test: no prompt templates, so the hashing
// embedder sees the raw chunk and query text. In production you would inject
// semanticsearch.NewModel(...) instead.
type plainModel struct{ dim int }

func (m plainModel) Name() string    { return "hashing" }
func (m plainModel) Dimensions() int { return m.dim }
func (plainModel) BuildData(chunk storage.Chunk) string {
	if chunk.Title == "" {
		return chunk.Text
	}
	return chunk.Title + " " + chunk.Text
}
func (plainModel) BuildQuery(query, taskType string) (string, error) { return query, nil }

// hashingEmbedder is a deterministic, in-process embedder for the end-to-end test: it hashes
// each token into a fixed-size vector, so texts that share words end up close together. It
// needs no model or server, so this test runs anywhere. In production you would inject
// semanticsearch.NewAiEmbedder(...) instead — the composition above is otherwise identical.
type hashingEmbedder struct{ dim int }

func (h hashingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector := make([]float32, h.dim)
		for _, token := range tokenize(text) {
			hasher := fnv.New32a()
			_, _ = hasher.Write([]byte(token))
			vector[int(hasher.Sum32()%uint32(h.dim))]++
		}
		vectors[i] = vector
	}

	return vectors, nil
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func writeFixtures(t *testing.T, dir string) {
	t.Helper()

	write(t, dir, "vacation.txt", "Full-time employees receive fifteen paid vacation days annually, rising to twenty after five years.")
	write(t, dir, "billing.md", "# Billing\n\n## Refunds\n\nA refund is returned to the original payment method within five business days.")
	write(t, dir, "loader.go", "package app\n\n// ReadConfig loads the configuration file from disk and parses its settings.\nfunc ReadConfig(path string) (Config, error) {\n\treturn parseConfig(path)\n}\n")
	writeDocx(t, filepath.Join(dir, "handbook.docx"), "Staff may work remotely from home up to three days per week with manager approval.")
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeDocx builds a minimal .docx (a ZIP holding word/document.xml) with the given body text.
func writeDocx(t *testing.T, path, body string) {
	t.Helper()
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body><w:p><w:r><w:t>` + body + `</w:t></w:r></w:p></w:body></w:document>`

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create docx part: %v", err)
	}
	if _, err := w.Write([]byte(document)); err != nil {
		t.Fatalf("write docx part: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
}
