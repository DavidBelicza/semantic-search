package indexer

import (
	"context"

	"semantic-search/internal/crawler"
)

const documentUpsertBatchSize = 500

type DocumentStore interface {
	UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error
}

func IndexPath(ctx context.Context, store DocumentStore, rootPath string) error {
	files, err := crawler.CollectFileMetadata(rootPath)
	if err != nil {
		return err
	}

	if err := upsertDocumentsInBatches(ctx, store, files); err != nil {
		return err
	}

	return nil
}

func upsertDocumentsInBatches(ctx context.Context, store DocumentStore, files []crawler.FileMetadata) error {
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
