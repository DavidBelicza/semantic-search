package cmd

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"semantic-search/internal/indexer"
	"semantic-search/internal/scanner"
	"semantic-search/internal/strategy"
)

type IndexStore interface {
	indexer.MetadataStore
	scanner.Store
	strategy.Store
}

func NewIndexCommand(out io.Writer, store IndexStore, vectorStore strategy.VectorStore) *cobra.Command {
	return NewIndexCommandWithPool(out, store, vectorStore, strategy.DefaultPool())
}

func NewIndexCommandWithPool(out io.Writer, store IndexStore, vectorStore strategy.VectorStore, strategyPool strategy.Pool) *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index Markdown files from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if store == nil {
				return errors.New("document store is required")
			}
			if vectorStore == nil {
				return errors.New("vector store is required")
			}

			ctx := context.Background()
			if err := indexer.IndexPath(ctx, store, args[0], strategyPool); err != nil {
				return err
			}

			if _, err := scanner.ScanIndexedDocuments(ctx, store); err != nil {
				return err
			}

			if _, err := strategy.ProcessScannedDocuments(ctx, store, vectorStore, strategyPool); err != nil {
				return err
			}

			_, err := strategy.ProcessChunkedDocuments(ctx, store, vectorStore, strategyPool)
			return err
		},
	}

	indexCmd.SetOut(out)
	indexCmd.SetErr(out)

	return indexCmd
}
