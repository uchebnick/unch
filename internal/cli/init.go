package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/semsearch"
)

func runInit(ctx context.Context, program string, args []string, cwd string) error {
	_ = ctx
	_ = cwd

	fs := flag.NewFlagSet(program+" init", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory to initialize")
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

	paths, _, err := semsearch.Init(rootAbs)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "Initialized %s\n", paths.LocalDir)
	return nil
}

func resolveInitRoot(rootFlag string, positionalArgs []string) (string, error) {
	rootFlag = strings.TrimSpace(rootFlag)
	switch len(positionalArgs) {
	case 0:
		if rootFlag == "" {
			return ".", nil
		}
		return rootFlag, nil
	case 1:
		if rootFlag != "" && rootFlag != "." {
			return "", fmt.Errorf("pass either a positional root or --root, not both")
		}
		return positionalArgs[0], nil
	default:
		return "", fmt.Errorf("init accepts at most one positional path")
	}
}
