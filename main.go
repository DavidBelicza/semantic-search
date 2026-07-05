package main

import (
	"fmt"
	"os"

	"github.com/davidbelicza/semantic-search/cmd"
)

func main() {
	if err := cmd.Execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
