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

	appembed "github.com/uchebnick/unch/internal/embed"
	unchmcp "github.com/uchebnick/unch/internal/mcp"
)

func runStart(ctx context.Context, program string, args []string, cwd string) error {
	_ = cwd

	if len(args) == 0 {
		return fmt.Errorf("start requires a target, for example: mcp")
	}
	if isHelpArg(args[0]) {
		return printStartHelp(os.Stdout, cliName(program))
	}

	switch args[0] {
	case "mcp":
		return runStartMCP(ctx, program, args[1:])
	default:
		return fmt.Errorf("unknown start target %q", args[0])
	}
}

func runStartMCP(ctx context.Context, program string, args []string) error {
	defaultModelPath := defaultModelFlagValue()

	fs := flag.NewFlagSet(program+" start mcp", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.Usage = func() {}

	root := fs.String("root", ".", "repository root served by the MCP process")
	stateDir := fs.String("state-dir", "", "path to .semsearch directory; defaults to <root>/.semsearch")
	dbPath := fs.String("db", "", "deprecated: path to .semsearch/index.db, or to a .semsearch directory")
	modelPath := fs.String("model", defaultModelPath, "path to GGUF embedding model, or a known model id such as embeddinggemma or qwen3")
	provider := fs.String("provider", appembed.DefaultProvider().String(), "embedding provider: llama.cpp or openrouter")
	libPath := fs.String("lib", "", "path to yzma library directory, or to one of its shared library files")
	contextSize := fs.Int("ctx-size", 0, "llama context size; 0 uses the selected model default")
	verbose := fs.Bool("verbose", false, "enable yzma verbose logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" start mcp [flags]",
				"Start an MCP stdio server for one repository workspace.",
				[]string{
					cliName(program) + " start mcp",
					cliName(program) + " start mcp --root ~/src/repo --state-dir ~/src/repo/.semsearch",
					cliName(program) + " start mcp --root . --model qwen3",
					cliName(program) + " start mcp --provider openrouter --model openai/text-embedding-3-small",
				},
				[]string{
					"From the repository root, `unch start mcp` is enough in the common case.",
					"Stdout is reserved for MCP protocol messages; use this command as a child process from an MCP client.",
					"The server exposes workspace_status, search_code, and index_repository tools.",
					"Use --provider openrouter with --model <remote-model-id>; token lookup checks OPENROUTER_API_KEY, then the global unch tokens.json, then .semsearch/tokens.json.",
					"Use one MCP process per repository workspace; the process reuses the same model/runtime configuration across tool calls.",
				},
			)
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected positional arguments for start mcp: %s", strings.Join(fs.Args(), " "))
	}

	stateDirWasExplicit := false
	dbWasExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "state-dir" {
			stateDirWasExplicit = true
		}
		if f.Name == "db" {
			dbWasExplicit = true
		}
	})

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	targetPaths, resolvedIndexPath, _, err := resolveStateTarget(rootAbs, *stateDir, stateDirWasExplicit, *dbPath, dbWasExplicit)
	if err != nil {
		return err
	}

	backend := newMCPBackend(mcpBackendConfig{
		RootAbs:           rootAbs,
		TargetPaths:       targetPaths,
		IndexPath:         resolvedIndexPath,
		RequestedProvider: strings.TrimSpace(*provider),
		RequestedModel:    strings.TrimSpace(*modelPath),
		RequestedLibPath:  strings.TrimSpace(*libPath),
		ContextSize:       *contextSize,
		Verbose:           *verbose,
	})
	defer func() {
		_ = backend.Close()
	}()

	server := unchmcp.NewServer(backend)
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

func printStartHelp(w io.Writer, program string) error {
	_, err := fmt.Fprintf(
		w,
		"Usage:\n  %s start mcp [flags]\n\nTargets:\n  mcp  Start an MCP stdio server for one repository workspace\n\nUse `%s start mcp --help` for flags.\n",
		program,
		program,
	)
	return err
}
