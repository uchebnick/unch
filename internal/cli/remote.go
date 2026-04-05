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
	if isHelpArg(args[0]) {
		return printRemoteHelp(os.Stdout, cliName(program))
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
	fs.Usage = func() {}

	rootFlag := fs.String("root", ".", "root directory whose remote search index should be refreshed")
	allowMissing := fs.Bool("allow-missing", false, "continue when the published remote index is unavailable or incompatible")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" remote sync [flags] [root]",
				"Refresh the local index from a bound remote GitHub Actions workflow.",
				[]string{
					cliName(program) + " remote sync",
					cliName(program) + " remote sync --root ../repo --allow-missing",
				},
				[]string{
					"If the manifest is not bound, this command reports that and exits cleanly.",
					"Use --allow-missing in CI bootstrap flows so older or unpublished remote indexes do not fail the run.",
				},
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

	paths, err := semsearch.PreparePaths(rootAbs)
	if err != nil {
		return err
	}

	result, err := semsearch.SyncRemoteIndex(ctx, paths.LocalDir)
	if err != nil {
		if *allowMissing {
			if errors.Is(err, semsearch.ErrRemoteIndexNotPublished) {
				_, _ = fmt.Fprintln(os.Stdout, "Remote index is not published yet; run the remote index workflow once to publish it")
				return nil
			}
			if errors.Is(err, semsearch.ErrRemoteIndexIncompatible) {
				_, _ = fmt.Fprintln(os.Stdout, "Remote index uses an older schema; continuing without restore so the remote index workflow can rebuild and republish it")
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
	fs.Usage = func() {}

	rootFlag := fs.String("root", ".", "root directory where the downloaded search index should be written")
	commitFlag := fs.String("commit", "", "commit SHA whose search index artifact should be downloaded")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" remote download [flags] --commit <sha> <github-repo-or-workflow-url>",
				"Download a published search artifact for a specific commit without binding the repository to remote sync.",
				[]string{
					cliName(program) + " remote download --commit abc123 https://github.com/uchebnick/unch",
					cliName(program) + " remote download --commit abc123 https://github.com/uchebnick/unch/actions/workflows/searcher.yml",
				},
				[]string{
					"The downloaded manifest is activated as local-only state, so future searches do not auto-sync to latest remote by accident.",
				},
			)
		}
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
