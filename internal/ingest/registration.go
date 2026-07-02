package ingest

import (
	"context"
	storage "semantic-search/internal/storage/sqlite"
)

const documentUpsertBatchSize = 500

type MetadataStore interface {
	UpsertDocuments(ctx context.Context, files []storage.FileMetadata) error
}

type FileSupport interface {
	Supports(path string) bool
}

// Register records discovered files as documents: it drops the files no strategy
// supports and upserts the rest in batches. It takes the files as input (the pipeline
// discovers them) so this step does not depend on the discovery step.
func Register(ctx context.Context, store MetadataStore, support FileSupport, files []storage.FileMetadata) error {
	return upsertDocumentsInBatches(ctx, store, supportedFiles(files, support))
}

func supportedFiles(files []storage.FileMetadata, support FileSupport) []storage.FileMetadata {
	if support == nil {
		return nil
	}

	supported := make([]storage.FileMetadata, 0, len(files))
	for _, file := range files {
		if support.Supports(file.AbsolutePath) {
			supported = append(supported, file)
		}
	}

	return supported
}

func upsertDocumentsInBatches(ctx context.Context, store MetadataStore, files []storage.FileMetadata) error {
	for start := 0; start < len(files); start += documentUpsertBatchSize {
		end := start + documentUpsertBatchSize
		if end > len(files) {
			end = len(files)
		}

		if err := store.UpsertDocuments(ctx, files[start:end]); err != nil {
			return err
		}
	}

	return nil
}
