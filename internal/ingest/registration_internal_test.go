package ingest

import (
	"context"
	"reflect"
	storage "semantic-search/internal/storage/sqlite"
	"strings"
	"testing"
)

// markdownSupport is a minimal FileSupport for the filter test. Using a local fake keeps
// this internal test free of the strategy package, which imports storage, which imports
// this package — importing it here would create a test import cycle.
type markdownSupport struct{}

func (markdownSupport) Supports(path string) bool {
	return strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".markdown")
}

func TestUpsertDocumentsInBatchesUsesFixedBatchSize(t *testing.T) {
	files := make([]storage.FileMetadata, documentUpsertBatchSize*2+1)
	store := &recordingDocumentStore{}

	if err := upsertDocumentsInBatches(context.Background(), store, files); err != nil {
		t.Fatalf("upsert documents in batches: %v", err)
	}

	want := []int{documentUpsertBatchSize, documentUpsertBatchSize, 1}
	if !reflect.DeepEqual(store.batchSizes, want) {
		t.Fatalf("batch sizes mismatch\nwant: %#v\n got: %#v", want, store.batchSizes)
	}
}

func TestSupportedFilesFiltersUnsupportedFiles(t *testing.T) {
	files := []storage.FileMetadata{
		{AbsolutePath: "/tmp/a.md"},
		{AbsolutePath: "/tmp/b.txt"},
		{AbsolutePath: "/tmp/c.markdown"},
	}

	got := supportedFiles(files, markdownSupport{})
	if len(got) != 2 {
		t.Fatalf("supported file count mismatch: want 2, got %d", len(got))
	}
	if got[0].AbsolutePath != "/tmp/a.md" || got[1].AbsolutePath != "/tmp/c.markdown" {
		t.Fatalf("supported files mismatch: %#v", got)
	}
}

type recordingDocumentStore struct {
	batchSizes []int
}

func (s *recordingDocumentStore) UpsertDocuments(ctx context.Context, files []storage.FileMetadata) error {
	s.batchSizes = append(s.batchSizes, len(files))
	return nil
}
