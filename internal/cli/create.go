package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uchebnick/unch-searcher/internal/semsearch"
)

func runCreate(ctx context.Context, program string, args []string, cwd string) error {
	_ = ctx
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("create requires a target, for example: ci")
	}

	switch args[0] {
	case "ci":
		return runCreateCI(program, args[1:])
	default:
		return fmt.Errorf("unknown create target %q", args[0])
	}
}

func runCreateCI(program string, args []string) error {
	fs := flag.NewFlagSet(program+" create ci", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory where .github/workflows/searcher.yml will be created")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootInput, err := resolveInitRoot(*rootFlag, fs.Args())
	if err != nil {
		return err
	}

	rootAbs, err := filepath.Abs(rootInput)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	workflowPath, created, err := semsearch.EnsureCIWorkflow(rootAbs)
	if err != nil {
		return err
	}

	if created {
		_, _ = fmt.Fprintf(os.Stdout, "Created %s\n", workflowPath)
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "Already exists %s\n", workflowPath)
	return nil
}
