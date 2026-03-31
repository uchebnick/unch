package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uchebnick/unch-searcher/internal/semsearch"
)

func runRemote(ctx context.Context, program string, args []string, cwd string) error {
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("remote requires a target, for example: bind or sync")
	}

	switch args[0] {
	case "bind":
		return runBindCI(program, args[1:])
	case "sync":
		return runRemoteSync(ctx, program, args[1:])
	default:
		return fmt.Errorf("unknown remote target %q", args[0])
	}
}

func runRemoteSync(ctx context.Context, program string, args []string) error {
	fs := flag.NewFlagSet(program+" remote sync", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory whose remote search index should be refreshed")
	allowMissing := fs.Bool("allow-missing", false, "treat an unpublished remote index as a non-fatal condition")
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

	paths, err := semsearch.PreparePaths(rootAbs)
	if err != nil {
		return err
	}

	result, err := semsearch.SyncRemoteIndex(ctx, paths.LocalDir)
	if err != nil {
		if *allowMissing && errors.Is(err, semsearch.ErrRemoteIndexNotPublished) {
			_, _ = fmt.Fprintln(os.Stdout, "Remote index is not published yet; run the searcher GitHub Actions workflow once to publish it")
			return nil
		}
		return err
	}

	if !result.Checked {
		_, _ = fmt.Fprintf(os.Stdout, "Manifest %s is not bound to a remote workflow\n", paths.ManifestPath)
		return nil
	}

	if result.Note != "" {
		_, _ = fmt.Fprintln(os.Stdout, result.Note)
	}
	return nil
}
