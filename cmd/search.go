package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	semanticsearch "github.com/davidbelicza/semantic-search/pkg"
)

func newSearchCommand(databasePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "search [limit] [query]",
		Short: "Search indexed Markdown content",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid limit %q: %w", args[0], err)
			}

			results, err := semanticsearch.Search(context.Background(), *databasePath, args[1], limit)
			if err != nil {
				return err
			}

			writeSearchResults(cmd.OutOrStdout(), results)
			return nil
		},
	}
}

func writeSearchResults(out io.Writer, results []semanticsearch.SearchResult) {
	if len(results) == 0 {
		fmt.Fprintln(out, "No results.")
		return
	}

	for i, result := range results {
		fmt.Fprintf(out, "%d. document_id=%d chunk_id=%d score=%.4f\n", i+1, result.DocumentID, result.ChunkID, result.Score)
		if result.Title != "" {
			fmt.Fprintf(out, "   [%s]\n", result.Title)
		}
		fmt.Fprintf(out, "   %s\n", result.Text)
	}
}
