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

func TestPostgresDocumentsFromIDAndDelete(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/a.txt", SizeBytes: 1, ModifiedAtNS: 1},
		{FileID: "f2", AbsolutePath: "/b.txt", SizeBytes: 1, ModifiedAtNS: 1},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	all, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("documents from id: %v %+v", err, all)
	}

	target := all[0]
	if _, err := store.ApplyDocumentChunkReconcile(ctx, target.ID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hi", TokenCount: 1, ContentHash: "h1"}},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if err := store.DeleteDocument(ctx, target.ID); err != nil {
		t.Fatalf("delete document: %v", err)
	}

	remaining, err := store.DocumentsFromID(ctx, 0, 10)
	if err != nil || len(remaining) != 1 || remaining[0].ID == target.ID {
		t.Fatalf("expected the target document removed, got %v %+v", err, remaining)
	}
	chunks, err := store.ChunksByDocumentID(ctx, target.ID)
	if err != nil || len(chunks) != 0 {
		t.Fatalf("expected the document's chunks removed, got %v %+v", err, chunks)
	}
}

func TestPostgresChunkLookups(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	if err := store.UpsertDocuments(ctx, []storage.FileMetadata{
		{FileID: "f1", AbsolutePath: "/a.txt", SizeBytes: 1, ModifiedAtNS: 1},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	docs, err := store.DocumentsByStatus(ctx, "indexed", 0, 10)
	if err != nil || len(docs) != 1 {
		t.Fatalf("by status: %v %+v", err, docs)
	}
	docID := docs[0].ID

	inserted, err := store.ApplyDocumentChunkReconcile(ctx, docID, storage.ChunkReconcilePlan{
		Insert: []storage.Chunk{{ChunkIndex: 0, Title: "Intro", Text: "hi", ContentHash: "h1"}},
	})
	if err != nil || len(inserted) != 1 {
		t.Fatalf("reconcile: %v %+v", err, inserted)
	}
	chunkID := inserted[0].ID

	mapping, err := store.ChunkDocumentIDs(ctx, []int64{chunkID})
	if err != nil || len(mapping) != 1 || mapping[0].DocumentID != docID {
		t.Fatalf("chunk document ids: %v %+v", err, mapping)
	}
	byIDs, err := store.DocumentsByIDs(ctx, []int64{docID})
	if err != nil || len(byIDs) != 1 || byIDs[0].AbsolutePath != "/a.txt" {
		t.Fatalf("documents by ids: %v %+v", err, byIDs)
	}

	// Empty inputs hit the early-return guards.
	if m, err := store.ChunkDocumentIDs(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("empty mapping: %v %+v", err, m)
	}
	if d, err := store.DocumentsByIDs(ctx, nil); err != nil || len(d) != 0 {
		t.Fatalf("empty documents: %v %+v", err, d)
	}
}
