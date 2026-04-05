package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uchebnick/unch/internal/semsearch"
)

func runCreate(ctx context.Context, program string, args []string, cwd string) error {
	_ = ctx
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("create requires a target, for example: ci")
	}
	if isHelpArg(args[0]) {
		return printCreateHelp(os.Stdout, cliName(program))
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
	fs.Usage = func() {}

	rootFlag := fs.String("root", ".", "root directory where the remote index workflow (.github/workflows/unch-index.yml) will be created")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" create ci [flags]",
				"Create the remote index GitHub Actions workflow in the target repository.",
				[]string{
					cliName(program) + " create ci",
					cliName(program) + " create ci --root ../other-repo",
				},
				nil,
			)
		}
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
