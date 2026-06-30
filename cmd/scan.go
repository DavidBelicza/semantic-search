package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	semanticsearch "semantic-search/pkg"
)

func NewScanCommand(out io.Writer, store semanticsearch.AppStore) *cobra.Command {
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan indexed files for content changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return semanticsearch.Scan(context.Background(), store)
		},
	}

	scanCmd.SetOut(out)
	scanCmd.SetErr(out)

	return scanCmd
}
