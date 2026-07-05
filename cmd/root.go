package cmd

import (
	"io"

	"github.com/spf13/cobra"
)

const appName = "semantic-search"
const defaultDatabasePath = "vector-index.db"

// Execute builds the root command and runs it. cmd only parses CLI input and proxies to
// pkg; it instantiates nothing itself.
func Execute(args []string, stdout io.Writer, stderr io.Writer) error {
	return newRootCommand(stdout, stderr, args).Execute()
}

func newRootCommand(stdout io.Writer, stderr io.Writer, args []string) *cobra.Command {
	var databasePath string

	rootCmd := &cobra.Command{
		Use:   appName,
		Short: "Local Markdown semantic search indexer",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&databasePath, "db", defaultDatabasePath, "SQLite database path")
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(newIndexCommand(&databasePath))
	rootCmd.AddCommand(newSearchCommand(&databasePath))

	return rootCmd
}
