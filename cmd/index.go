package cmd

import (
	"context"

	"github.com/spf13/cobra"

	semanticsearch "semantic-search/pkg"
)

func newIndexCommand(databasePath *string) *cobra.Command {
	var options semanticsearch.IndexOptions

	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index Markdown files from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return semanticsearch.Index(context.Background(), *databasePath, args[0], options)
		},
	}

	indexCmd.Flags().BoolVar(&options.FailFast, "fail-fast", false, "Abort on the first document error instead of continuing")
	indexCmd.Flags().BoolVar(&options.IncludeHidden, "include-hidden", false, "Index hidden files and directories")
	indexCmd.Flags().BoolVar(&options.FollowSymlinks, "follow-symlinks", false, "Follow symbolic links")

	return indexCmd
}
