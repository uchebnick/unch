package main

import (
	"fmt"
	"os"

	"unch_code_searcher/internal"
)

func main() {
	if err := internal.RunCLI(os.Args[0], os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
