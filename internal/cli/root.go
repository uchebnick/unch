package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/runtime"
)

// Run initializes the CLI runtime and dispatches to the selected subcommand.
func Run(program string, args []string) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working dir: %w", err)
	}

	command, commandArgs, err := detectCommand(args)
	if err != nil {
		return err
	}
	if command == "help" {
		return runHelp(program, commandArgs)
	}
	if command == "version" {
		return printVersion(os.Stdout)
	}
	if command == "init" {
		return runInit(ctx, program, commandArgs, cwd)
	}
	if command == "bind" {
		return runBind(ctx, program, commandArgs, cwd)
	}
	if command == "create" {
		return runCreate(ctx, program, commandArgs, cwd)
	}
	if command == "remote" {
		return runRemote(ctx, program, commandArgs, cwd)
	}

	scanner := indexing.FileScanner{}
	models := runtime.ModelCache{}
	runtimes := runtime.YzmaResolver{}

	switch command {
	case "search":
		return runSearch(ctx, program, commandArgs, cwd, scanner, runtimes, models)
	default:
		return runIndex(ctx, program, commandArgs, cwd, scanner, runtimes, models)
	}
}

func detectCommand(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "index", args, nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		return "help", args[1:], nil
	case "-version", "--version", "version":
		return "version", args[1:], nil
	case "bind", "create", "init", "index", "remote", "search":
		return args[0], args[1:], nil
	default:
		if len(args[0]) > 0 && args[0][0] == '-' {
			return "index", args, nil
		}
		return "", nil, fmt.Errorf("unknown command %q", args[0])
	}
}
