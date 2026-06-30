package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	semanticsearch "semantic-search/pkg"
)

func NewRebuildCommand(out io.Writer, store semanticsearch.AppStore, vectorStore semanticsearch.VectorStore) *cobra.Command {
	return NewRebuildCommandWithPool(out, store, vectorStore, semanticsearch.DefaultStrategyPool())
}

func NewRebuildCommandWithPool(out io.Writer, store semanticsearch.AppStore, vectorStore semanticsearch.VectorStore, strategyPool semanticsearch.StrategyPool) *cobra.Command {
	rebuildCmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild the vector index from indexed documents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return semanticsearch.Rebuild(context.Background(), store, vectorStore, strategyPool)
		},
	}

	rebuildCmd.SetOut(out)
	rebuildCmd.SetErr(out)

	return rebuildCmd
}
