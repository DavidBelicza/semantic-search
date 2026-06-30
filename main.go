package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"semantic-search/cmd"
	"semantic-search/internal/embedder"
	"semantic-search/internal/storage"
	"semantic-search/internal/vectorstore"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	databasePath, err := databasePathFromArgs(args)
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

	vectorStore, err := vectorstore.Open(context.Background(), vectorDatabasePath(databasePath), embedder.DefaultDimensions)
	if err != nil {
		return err
	}
	defer vectorStore.Close()

	rootCmd := cmd.NewRootCommand(stdout, store, vectorStore)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	return rootCmd.Execute()
}

func databasePathFromArgs(args []string) (string, error) {
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

func vectorDatabasePath(databasePath string) string {
	extension := filepath.Ext(databasePath)
	if extension == "" {
		return databasePath + ".lancedb"
	}

	return strings.TrimSuffix(databasePath, extension) + ".lancedb"
}
