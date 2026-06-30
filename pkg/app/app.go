package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"semantic-search/cmd"
	"semantic-search/internal/embedder"
	"semantic-search/internal/storage/lancedb"
	storage "semantic-search/internal/storage/sqlite"
)

func Run(args []string, stdout io.Writer, stderr io.Writer) error {
	databasePath, err := DatabasePathFromArgs(args)
	if err != nil {
		return err
	}

	store, err := storage.Open(databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		return err
	}

	vectorStore, err := lancedb.Open(context.Background(), VectorDatabasePath(databasePath), embedder.DefaultDimensions)
	if err != nil {
		return err
	}
	defer vectorStore.Close()

	rootCmd := cmd.NewRootCommand(stdout, store, vectorStore)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	return rootCmd.Execute()
}

func DatabasePathFromArgs(args []string) (string, error) {
	for i, arg := range args {
		if arg == "--db" {
			if i+1 >= len(args) {
				return "", errors.New("missing value for --db")
			}

			return args[i+1], nil
		}

		if strings.HasPrefix(arg, "--db=") {
			databasePath := strings.TrimPrefix(arg, "--db=")
			if databasePath == "" {
				return "", errors.New("missing value for --db")
			}

			return databasePath, nil
		}
	}

	return cmd.DefaultDatabasePath, nil
}

func VectorDatabasePath(databasePath string) string {
	extension := filepath.Ext(databasePath)
	if extension == "" {
		return databasePath + ".lancedb"
	}

	return strings.TrimSuffix(databasePath, extension) + ".lancedb"
}
