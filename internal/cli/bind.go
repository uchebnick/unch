package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch-searcher/internal/semsearch"
)

func runBind(ctx context.Context, program string, args []string, cwd string) error {
	_ = ctx
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("bind requires a target, for example: ci")
	}

	switch args[0] {
	case "ci":
		return runBindCI(program, args[1:])
	default:
		return fmt.Errorf("unknown bind target %q", args[0])
	}
}

func runBindCI(program string, args []string) error {
	fs := flag.NewFlagSet(program+" bind ci", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory whose manifest should be bound to a GitHub repository or CI workflow")
	if err := fs.Parse(args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("bind ci requires exactly one GitHub repository or workflow URL")
	}

	ciURL := strings.TrimSpace(remaining[0])
	if ciURL == "" {
		return fmt.Errorf("bind ci requires a non-empty GitHub repository or workflow URL")
	}

	rootAbs, err := filepath.Abs(*rootFlag)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	paths, _, err := semsearch.Init(rootAbs)
	if err != nil {
		return err
	}

	manifest, err := semsearch.BindRemoteManifest(paths.LocalDir, ciURL)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "Bound %s to %s (version %d)\n", paths.ManifestPath, manifest.Remote.CIURL, manifest.Version)
	return nil
}
