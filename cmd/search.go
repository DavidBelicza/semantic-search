package cmd

import (
	"io"

	"github.com/spf13/cobra"
)

func NewSearchCommand(out io.Writer) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search indexed Markdown content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	searchCmd.SetOut(out)
	searchCmd.SetErr(out)

	return searchCmd
}
