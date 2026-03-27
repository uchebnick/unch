package main

import (
	"fmt"
	"os"

	"github.com/uchebnick/unch-searcher/internal"
)

func main() {
	if err := internal.RunCLI(os.Args[0], os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
