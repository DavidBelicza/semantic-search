package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"semantic-search/internal/embedder"
	semanticsearch "semantic-search/pkg"
)

func NewSearchCommand(out io.Writer, store semanticsearch.SearchMetadataStore, vectorStore semanticsearch.SearchVectorStore) *cobra.Command {
	return NewSearchCommandWithEmbedder(out, store, vectorStore, defaultQueryEmbedder())
}

func NewSearchCommandWithEmbedder(out io.Writer, store semanticsearch.SearchMetadataStore, vectorStore semanticsearch.SearchVectorStore, queryEmbedder semanticsearch.QueryEmbedder) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search [limit] [query]",
		Short: "Search indexed Markdown content",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid limit %q: %w", args[0], err)
			}

			results, err := semanticsearch.Search(context.Background(), store, vectorStore, queryEmbedder, args[1], limit)
			if err != nil {
				return err
			}

			writeSearchResults(cmd.OutOrStdout(), results)
			return nil
		},
	}

	searchCmd.SetOut(out)
	searchCmd.SetErr(out)

	return searchCmd
}

func defaultQueryEmbedder() semanticsearch.QueryEmbedder {
	queryEmbedder := embedder.NewOpenAIEmbedder(embedder.DefaultBaseURL, embedder.DefaultModel)
	queryEmbedder.Dimensions = embedder.DefaultDimensions
	queryEmbedder.Prefix = embedder.QueryPrefix
	return queryEmbedder
}

func writeSearchResults(out io.Writer, results []semanticsearch.SearchResult) {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return
	}

	for i, result := range results {
		fmt.Fprintf(out, "%d. document_id=%d chunk_id=%d score=%.4f\n", i+1, result.DocumentID, result.ChunkID, result.Score)
		fmt.Fprintf(out, "   %s\n", result.Text)
	}
}
