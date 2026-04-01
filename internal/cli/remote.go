package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/semsearch"
)

func runRemote(ctx context.Context, program string, args []string, cwd string) error {
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("remote requires a target, for example: bind, sync, or download")
	}

	switch args[0] {
	case "bind":
		return runBindCI(program, args[1:])
	case "sync":
		return runRemoteSync(ctx, program, args[1:])
	case "download":
		return runRemoteDownload(ctx, program, args[1:])
	default:
		return fmt.Errorf("unknown remote target %q", args[0])
	}
}

func runRemoteSync(ctx context.Context, program string, args []string) error {
	fs := flag.NewFlagSet(program+" remote sync", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory whose remote search index should be refreshed")
	allowMissing := fs.Bool("allow-missing", false, "continue when the published remote index is unavailable or incompatible")
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
		if *allowMissing {
			if errors.Is(err, semsearch.ErrRemoteIndexNotPublished) {
				_, _ = fmt.Fprintln(os.Stdout, "Remote index is not published yet; run the searcher GitHub Actions workflow once to publish it")
				return nil
			}
			if errors.Is(err, semsearch.ErrRemoteIndexIncompatible) {
				_, _ = fmt.Fprintln(os.Stdout, "Remote index uses an older schema; continuing without restore so the searcher workflow can rebuild and republish it")
				return nil
			}
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

func runRemoteDownload(ctx context.Context, program string, args []string) error {
	fs := flag.NewFlagSet(program+" remote download", flag.ContinueOnError)
	fs.SetOutput(nil)

	rootFlag := fs.String("root", ".", "root directory where the downloaded search index should be written")
	commitFlag := fs.String("commit", "", "commit SHA whose search index artifact should be downloaded")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*commitFlag) == "" {
		return fmt.Errorf("remote download requires --commit <sha>")
	}

	targetArgs := fs.Args()
	if len(targetArgs) != 1 {
		return fmt.Errorf("remote download requires exactly one GitHub repository or workflow URL")
	}

	rootAbs, err := filepath.Abs(*rootFlag)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	paths, _, err := semsearch.Init(rootAbs)
	if err != nil {
		return err
	}

	result, err := semsearch.DownloadIndexArtifactForCommit(ctx, paths.LocalDir, targetArgs[0], *commitFlag)
	if err != nil {
		return err
	}

	if result.Note != "" {
		_, _ = fmt.Fprintln(os.Stdout, result.Note)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Activated local search index at %s\n", filepath.Join(paths.LocalDir, "index.db"))
	return nil
}
