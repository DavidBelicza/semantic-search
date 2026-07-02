package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	storage "semantic-search/internal/storage/sqlite"
)

const fingerprintBatchSize = 1

type Store interface {
	DocumentsByStatus(ctx context.Context, status string, afterID int64, limit int) ([]storage.Document, error)
	UpdateDocumentContentHashAndStatus(ctx context.Context, fileID string, contentHash string, status string) error
	UpdateDocumentScanCheckpointAndStatus(ctx context.Context, fileID string, status string) error
}

type Result struct {
	Scanned int
}

// FingerprintIndexedDocuments hashes indexed documents one by one. With failFast unset, a
// per-document failure is recorded and scanning continues; the collected errors are
// joined and returned once every document has been visited. Pagination is by ascending
// id so a failed (and therefore still-indexed) document is not revisited in this run.
func FingerprintIndexedDocuments(ctx context.Context, store Store, failFast bool) (Result, error) {
	var result Result
	var errs []error
	var afterID int64

	for {
		documents, err := store.DocumentsByStatus(ctx, storage.DocumentStatusIndexed, afterID, fingerprintBatchSize)
		if err != nil {
			return result, err
		}
		if len(documents) == 0 {
			return result, errors.Join(errs...)
		}

		for _, document := range documents {
			afterID = document.ID
			status, err := fingerprintDocument(ctx, store, document)
			if err != nil && failFast {
				return result, err
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if status == storage.DocumentStatusScanned {
				result.Scanned++
			}
		}
	}
}

func fingerprintDocument(ctx context.Context, store Store, document storage.Document) (string, error) {
	if metadataMatchesCheckpoint(document) {
		return markCheckpoint(ctx, store, document, storage.DocumentStatusScanned)
	}

	contentHash, err := fingerprintFile(document.AbsolutePath)
	if err != nil {
		return "", fmt.Errorf("hash file %q: %w", document.AbsolutePath, err)
	}

	// Content is byte-identical to what has already been embedded (e.g. the file was
	// only touched). Restore the embedded status and skip re-chunking and
	// re-embedding entirely; the file was read once here just to hash it.
	if document.EmbeddedContentHash != "" && document.EmbeddedContentHash == contentHash {
		return markCheckpoint(ctx, store, document, storage.DocumentStatusEmbedded)
	}

	if document.HasHash && document.ContentHash == contentHash {
		return markCheckpoint(ctx, store, document, storage.DocumentStatusScanned)
	}

	if err := store.UpdateDocumentContentHashAndStatus(ctx, document.FileID, contentHash, storage.DocumentStatusScanned); err != nil {
		return "", fmt.Errorf("update scanned document %q: %w", document.AbsolutePath, err)
	}

	return storage.DocumentStatusScanned, nil
}

func metadataMatchesCheckpoint(document storage.Document) bool {
	return document.HasHash && document.HasScannedMetadata &&
		document.FileSize == document.ScannedFileSize &&
		document.ModifiedAtNS == document.ScannedModifiedAtNS
}

func markCheckpoint(ctx context.Context, store Store, document storage.Document, status string) (string, error) {
	if err := store.UpdateDocumentScanCheckpointAndStatus(ctx, document.FileID, status); err != nil {
		return "", fmt.Errorf("mark document %q as %s: %w", document.AbsolutePath, status, err)
	}

	return status, nil
}

func fingerprintFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
