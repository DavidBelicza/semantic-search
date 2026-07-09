package postgres

import (
	"context"
	"os"
	"testing"

	storage "github.com/davidbelicza/semantic-search/core/storage"
)

// testStore opens a store against SEMANTIC_SEARCH_POSTGRES_DSN and resets its schema. The test
// is skipped when the DSN is not set, so the default `go test` run needs no database.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SEMANTIC_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SEMANTIC_SEARCH_POSTGRES_DSN to run postgres integration tests")
	}

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS chunks, documents CASCADE"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema: %v", err)
	}

	return store
}

func TestPostgresDocumentAndChunkLifecycle(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/notes.txt", SizeBytes: 10, ModifiedAtNS: 100},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	docs, err := store.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil {
		t.Fatalf("by status: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("want 1 indexed document, got %d", len(docs))
	}
	documentID := docs[0].ID

	inserted, err := store.ApplyDocumentChunkReconcile(ctx, documentID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hello world", TokenCount: 2, ContentHash: "h1"}},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(inserted) != 1 || inserted[0].ID == 0 {
		t.Fatalf("expected one inserted chunk with an id, got %+v", inserted)
	}

	chunks, err := store.ChunksByDocumentID(ctx, documentID)
	if err != nil || len(chunks) != 1 || chunks[0].Text != "hello world" {
		t.Fatalf("chunks by document: %v %+v", err, chunks)
	}

	meta, err := store.ChunkMetadataByIDs(ctx, []int64{inserted[0].ID})
	if err != nil || len(meta) != 1 || meta[0].Title != "Intro" {
		t.Fatalf("chunk metadata: %v %+v", err, meta)
	}

	if err := store.MarkDocumentEmbedded(ctx, "f1", "h1"); err != nil {
		t.Fatalf("mark embedded: %v", err)
	}
	embedded, err := store.DocumentsByStatus(ctx, "embedded", 0, 10)
	if err != nil || len(embedded) != 1 || embedded[0].EmbeddedContentHash != "h1" {
		t.Fatalf("embedded documents: %v %+v", err, embedded)
	}
}

func TestPostgresUpdateMissingDocumentErrors(t *testing.T) {
	store := testStore(t)
	if err := store.UpdateDocumentStatus(context.Background(), "missing", "scanned"); err == nil {
		t.Fatal("expected error updating a missing document")
	}
}
