package cmd

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"semantic-search/internal/indexer"
)

func NewIndexCommand(out io.Writer, store indexer.DocumentStore) *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index Markdown files from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if store == nil {
				return errors.New("document store is required")
			}

			return indexer.IndexPath(context.Background(), store, args[0])
		},
	}

	indexCmd.SetOut(out)
	indexCmd.SetErr(out)

	return indexCmd
}
