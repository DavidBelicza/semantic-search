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

func NewIndexCommand(out io.Writer, store IndexStore) *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index Markdown files from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if store == nil {
				return errors.New("document store is required")
			}

			ctx := context.Background()
			strategyPool := strategy.DefaultPool()
			if err := indexer.IndexPath(ctx, store, args[0], strategyPool); err != nil {
				return err
			}

			if _, err := scanner.ScanIndexedDocuments(ctx, store); err != nil {
				return err
			}

			_, err := strategy.ProcessScannedDocuments(ctx, store, strategyPool)
			return err
		},
	}

	indexCmd.SetOut(out)
	indexCmd.SetErr(out)

	return indexCmd
}
