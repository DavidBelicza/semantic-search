package main

import (
	"fmt"
	"os"

	"semantic-search/cmd"
)

func main() {
	if err := cmd.Execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
