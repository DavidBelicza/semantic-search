package cmd

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"semantic-search/internal/scanner"
)

func NewScanCommand(out io.Writer, store scanner.Store) *cobra.Command {
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan indexed files for content changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if store == nil {
				return errors.New("document store is required")
			}

			_, err := scanner.ScanIndexedDocuments(context.Background(), store)
			return err
		},
	}

	scanCmd.SetOut(out)
	scanCmd.SetErr(out)

	return scanCmd
}
