package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	semanticsearch "semantic-search/pkg"
)

func NewIndexCommand(out io.Writer, store semanticsearch.IndexStore, vectorStore semanticsearch.VectorStore) *cobra.Command {
	return NewIndexCommandWithPool(out, store, vectorStore, semanticsearch.DefaultStrategyPool())
}

func NewIndexCommandWithPool(out io.Writer, store semanticsearch.IndexStore, vectorStore semanticsearch.VectorStore, strategyPool semanticsearch.StrategyPool) *cobra.Command {
	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index Markdown files from a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return semanticsearch.Index(context.Background(), store, vectorStore, strategyPool, args[0])
		},
	}

	indexCmd.SetOut(out)
	indexCmd.SetErr(out)

	return indexCmd
}
