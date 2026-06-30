package indexer

import (
	"context"

	"semantic-search/internal/crawler"
)

const documentUpsertBatchSize = 500

type MetadataStore interface {
	UpsertDocuments(ctx context.Context, files []crawler.FileMetadata) error
}

type FileSupport interface {
	Supports(path string) bool
}

func IndexPath(ctx context.Context, store MetadataStore, rootPath string, support FileSupport, options crawler.Options) error {
	files, err := crawler.CollectFileMetadata(rootPath, options)
	if err != nil {
		return err
	}

	files = supportedFiles(files, support)
	return upsertDocumentsInBatches(ctx, store, files)
}

func supportedFiles(files []crawler.FileMetadata, support FileSupport) []crawler.FileMetadata {
	if support == nil {
		return nil
	}

	supported := make([]crawler.FileMetadata, 0, len(files))
	for _, file := range files {
		if support.Supports(file.AbsolutePath) {
			supported = append(supported, file)
		}
	}

	return supported
}

func upsertDocumentsInBatches(ctx context.Context, store MetadataStore, files []crawler.FileMetadata) error {
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
