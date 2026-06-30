package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	vectorPath, err := vectorPathFromArgs(args)
	if err != nil {
		return err
	}

	store, err := storage.OpenWithExtensions(databasePath, extensionPaths(vectorPath))
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		return err
	}

	vectorStore := vectorstore.New(store.DB(), embedder.DefaultDimensions)
	if vectorPath != "" {
		if err := vectorStore.EnsureSchema(context.Background()); err != nil {
			return err
		}
	}

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

func vectorPathFromArgs(args []string) (string, error) {
	for i, arg := range args {
		if arg == "--vector" {
			if i+1 >= len(args) {
				return "", errors.New("missing value for --vector")
			}

			return args[i+1], nil
		}

		if strings.HasPrefix(arg, "--vector=") {
			vectorPath := strings.TrimPrefix(arg, "--vector=")
			if vectorPath == "" {
				return "", errors.New("missing value for --vector")
			}

			return vectorPath, nil
		}
	}

	return cmd.DefaultVectorPath, nil
}

func extensionPaths(vectorPath string) []string {
	if vectorPath == "" {
		return nil
	}

	return []string{normalizeExtensionPath(vectorPath)}
}

func normalizeExtensionPath(path string) string {
	for _, suffix := range []string{".dylib", ".so", ".dll"} {
		if strings.HasSuffix(path, suffix) {
			return strings.TrimSuffix(path, suffix)
		}
	}

	return path
}
