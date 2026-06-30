package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	storage "semantic-search/internal/storage/sqlite"
)

func TestHashFileReturnsSHA256Hex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("hash file: %v", err)
	}

	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("hash mismatch: want %q, got %q", want, got)
	}
}

func TestScanIndexedDocumentsMarksSameMetadataDoneWithoutHashing(t *testing.T) {
	store := &memoryScanStore{
		documents: []storage.Document{
			{
				FileID:              "1:100",
				AbsolutePath:        "/missing/file.md",
				FileSize:            10,
				ModifiedAtNS:        100,
				ContentHash:         "existing",
				HasHash:             true,
				ScannedFileSize:     10,
				ScannedModifiedAtNS: 100,
				HasScannedMetadata:  true,
				Status:              storage.DocumentStatusIndexed,
			},
		},
	}

	result, err := ScanIndexedDocuments(context.Background(), store)
	if err != nil {
		t.Fatalf("scan indexed documents: %v", err)
	}

	if result.Done != 1 || result.Scanned != 0 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if store.documents[0].Status != storage.DocumentStatusDone {
		t.Fatalf("status mismatch: want done, got %q", store.documents[0].Status)
	}
}

func TestScanIndexedDocumentsHashesAndMarksScannedWhenContentChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("new content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store := &memoryScanStore{
		documents: []storage.Document{
			{
				FileID:       "1:100",
				AbsolutePath: path,
				FileSize:     11,
				ModifiedAtNS: 100,
				ContentHash:  "old",
				HasHash:      true,
				Status:       storage.DocumentStatusIndexed,
			},
		},
	}

	result, err := ScanIndexedDocuments(context.Background(), store)
	if err != nil {
		t.Fatalf("scan indexed documents: %v", err)
	}

	if result.Done != 0 || result.Scanned != 1 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if store.documents[0].Status != storage.DocumentStatusScanned {
		t.Fatalf("status mismatch: want scanned, got %q", store.documents[0].Status)
	}
	if store.documents[0].ContentHash == "old" || store.documents[0].ContentHash == "" {
		t.Fatalf("content hash was not updated: %#v", store.documents[0])
	}
	if !store.documents[0].HasScannedMetadata {
		t.Fatal("scan metadata checkpoint was not updated")
	}
}

type memoryScanStore struct {
	documents []storage.Document
}

func (s *memoryScanStore) DocumentsByStatus(ctx context.Context, status string, limit int) ([]storage.Document, error) {
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

func (s *memoryScanStore) UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].ContentHash = contentHash
			s.documents[i].HasHash = true
			s.documents[i].ScannedFileSize = s.documents[i].FileSize
			s.documents[i].ScannedModifiedAtNS = s.documents[i].ModifiedAtNS
			s.documents[i].HasScannedMetadata = true
			s.documents[i].Status = status
		}
	}

	return nil
}

func (s *memoryScanStore) UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error {
	for i := range s.documents {
		if s.documents[i].FileID == fileID {
			s.documents[i].ScannedFileSize = s.documents[i].FileSize
			s.documents[i].ScannedModifiedAtNS = s.documents[i].ModifiedAtNS
			s.documents[i].HasScannedMetadata = true
			s.documents[i].Status = status
		}
	}

	return nil
}
