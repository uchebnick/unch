package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/semsearch"
)

func runAuth(ctx context.Context, program string, args []string, cwd string) error {
	_ = ctx
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("auth requires a target, for example: openrouter")
	}
	if isHelpArg(args[0]) {
		return printAuthHelp(os.Stdout, cliName(program))
	}

	switch args[0] {
	case "openrouter":
		return runAuthOpenRouter(program, args[1:])
	default:
		return fmt.Errorf("unknown auth target %q", args[0])
	}
}

func runAuthOpenRouter(program string, args []string) error {
	fs := flag.NewFlagSet(program+" auth openrouter", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.Usage = func() {}

	token := fs.String("token", "", "OpenRouter API key to store")
	local := fs.Bool("local", false, "store the token in .semsearch/tokens.json instead of the global unch config")
	root := fs.String("root", ".", "repository root used with --local")
	stateDir := fs.String("state-dir", "", "path to .semsearch directory used with --local; defaults to <root>/.semsearch")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" auth openrouter [flags]",
				"Save an OpenRouter API key for future unch commands.",
				[]string{
					cliName(program) + " auth openrouter --token sk-or-...",
					cliName(program) + " auth openrouter --token sk-or-... --local",
					"OPENROUTER_API_KEY=sk-or-... " + cliName(program) + " auth openrouter",
				},
				[]string{
					"By default this writes ~/.config/unch/tokens.json and keeps project state untouched.",
					"Use --local to write .semsearch/tokens.json for one repository workspace.",
					"OPENROUTER_API_KEY still overrides saved values at runtime when it is set.",
				},
			)
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected positional arguments for auth openrouter: %s", strings.Join(fs.Args(), " "))
	}

	tokenValue := strings.TrimSpace(*token)
	if tokenValue == "" {
		tokenValue = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if tokenValue == "" {
		return fmt.Errorf("missing token: pass --token or set OPENROUTER_API_KEY")
	}

	var targetPath string
	if *local {
		rootAbs, err := filepath.Abs(*root)
		if err != nil {
			return fmt.Errorf("resolve root: %w", err)
		}
		localDir := filepath.Join(rootAbs, ".semsearch")
		if trimmed := strings.TrimSpace(*stateDir); trimmed != "" {
			localDir, err = filepath.Abs(trimmed)
			if err != nil {
				return fmt.Errorf("resolve state dir: %w", err)
			}
		}
		if err := os.MkdirAll(localDir, 0o755); err != nil {
			return fmt.Errorf("create local state dir: %w", err)
		}
		targetPath = semsearch.LocalTokensPath(localDir)
	} else {
		globalPath, err := semsearch.GlobalTokensPath()
		if err != nil {
			return err
		}
		targetPath = globalPath
	}

	if err := semsearch.SaveProviderToken(targetPath, "openrouter", tokenValue); err != nil {
		return err
	}

	scope := "global"
	if *local {
		scope = "local"
	}
	_, _ = fmt.Fprintf(os.Stdout, "Saved %s OpenRouter token to %s\n", scope, targetPath)
	return nil
}

func printAuthHelp(w io.Writer, program string) error {
	_, err := fmt.Fprintf(
		w,
		"Usage:\n  %s auth openrouter [flags]\n\nTargets:\n  openrouter  Save an OpenRouter API key in global or local tokens.json\n\nUse `%s auth openrouter --help` for flags.\n",
		program,
		program,
	)
	return err
}
