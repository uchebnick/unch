package main

import (
	"fmt"
	"os"

	"github.com/uchebnick/unch/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[0], os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
