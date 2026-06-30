package cmd

import (
	"io"

	"github.com/spf13/cobra"

	"semantic-search/internal/indexer"
	"semantic-search/internal/scanner"
	"semantic-search/internal/strategy"
)

const appName = "semantic-search"
const DefaultDatabasePath = "vector-index.db"

type AppStore interface {
	indexer.MetadataStore
	scanner.Store
	strategy.Store
}

func NewRootCommand(out io.Writer, store AppStore, vectorStore strategy.VectorStore) *cobra.Command {
	var databasePath string

	rootCmd := &cobra.Command{
		Use:   appName,
		Short: "Local Markdown semantic search indexer",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&databasePath, "db", DefaultDatabasePath, "SQLite database path")
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)
	rootCmd.AddCommand(NewIndexCommand(out, store, vectorStore))
	rootCmd.AddCommand(NewScanCommand(out, store))
	rootCmd.AddCommand(NewSearchCommand(out))

	return rootCmd
}
