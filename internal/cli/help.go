package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/runtime"
)

const rootHelpWordmark = `
 _   _ _   _  ____ _   _
| | | | \ | |/ ___| | | |
| | | |  \| | |   | |_| |
| |_| | |\  | |___|  _  |
 \___/|_| \_|\____|_| |_|
`

func runHelp(program string, args []string) error {
	name := cliName(program)
	if len(args) == 0 {
		return printRootHelp(os.Stdout, name)
	}

	switch args[0] {
	case "index":
		return runIndex(context.TODO(), name, []string{"--help"}, ".", indexing.FileScanner{}, runtime.YzmaResolver{}, runtime.ModelCache{})
	case "search":
		return runSearch(context.TODO(), name, []string{"--help"}, ".", indexing.FileScanner{}, runtime.YzmaResolver{}, runtime.ModelCache{})
	case "init":
		return runInit(context.TODO(), name, []string{"--help"}, ".")
	case "create":
		if len(args) > 1 && args[1] == "ci" {
			return runCreate(context.TODO(), name, []string{"ci", "--help"}, ".")
		}
		return printCreateHelp(os.Stdout, name)
	case "bind":
		if len(args) > 1 && args[1] == "ci" {
			return runBind(context.TODO(), name, []string{"ci", "--help"}, ".")
		}
		return printBindHelp(os.Stdout, name)
	case "remote":
		if len(args) > 1 {
			switch args[1] {
			case "sync":
				return runRemote(context.TODO(), name, []string{"sync", "--help"}, ".")
			case "download":
				return runRemote(context.TODO(), name, []string{"download", "--help"}, ".")
			case "bind":
				return runRemote(context.TODO(), name, []string{"bind", "--help"}, ".")
			}
		}
		return printRemoteHelp(os.Stdout, name)
	default:
		return fmt.Errorf("unknown help topic %q", args[0])
	}
}

func printRootHelp(w io.Writer, program string) error {
	if _, err := fmt.Fprintln(w, helpWordmark()); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Semantic code search for code symbols and docs."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Usage:\n  %s <command> [flags]\n  %s [index flags]\n\n", program, program); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Commands:"); err != nil {
		return err
	}
	commands := []string{
		"  index   Build or refresh the local search index",
		"  search  Query the current index",
		"  init    Create .semsearch state in a repository",
		"  create  Generate helper files such as GitHub Actions workflow",
		"  bind    Bind the local manifest to a remote GitHub repo/workflow",
		"  remote  Sync or download published search indexes",
		"  help    Show root or command-specific help",
	}
	for _, line := range commands {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Model selection:"); err != nil {
		return err
	}
	modelNotes := []string{
		"  - Omit --model to auto-download the default GGUF embedding model: embeddinggemma-300m.",
		"  - Pass --model embeddinggemma or --model qwen3 to auto-select and auto-download a known GGUF model.",
		"  - Pass --model /path/to/model.gguf to use a custom GGUF embedding model.",
		"  - Known profiles today: embeddinggemma (mean pooling) and Qwen3-Embedding (last-token pooling).",
		"  - --ctx-size and --batch-size default to the selected model profile when left at 0.",
		"  - Rebuild the index after changing models, and use the same model family for both index and search.",
	}
	for _, line := range modelNotes {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Examples:"); err != nil {
		return err
	}
	examples := []string{
		fmt.Sprintf("  %s index --root .", program),
		fmt.Sprintf("  %s search \"sqlite schema\"", program),
		fmt.Sprintf("  %s search --details \"get path variables from a request\"", program),
		fmt.Sprintf("  %s index --model qwen3", program),
		fmt.Sprintf("  %s index --model ~/.semsearch/models/Qwen3-Embedding-0.6B-Q8_0.gguf", program),
		fmt.Sprintf("  %s help search", program),
	}
	for _, line := range examples {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func printCreateHelp(w io.Writer, program string) error {
	_, err := fmt.Fprintf(
		w,
		"Usage:\n  %s create ci [flags]\n\nTargets:\n  ci  Create the remote index workflow file (.github/workflows/searcher.yml) in the target repository\n\nUse `%s create ci --help` for flags.\n",
		program,
		program,
	)
	return err
}

func printBindHelp(w io.Writer, program string) error {
	_, err := fmt.Fprintf(
		w,
		"Usage:\n  %s bind ci [flags] <github-repo-or-workflow-url>\n\nTargets:\n  ci  Bind the local manifest to a remote GitHub repository or remote index workflow\n\nUse `%s bind ci --help` for flags.\n",
		program,
		program,
	)
	return err
}

func printRemoteHelp(w io.Writer, program string) error {
	_, err := fmt.Fprintf(
		w,
		"Usage:\n  %s remote <subcommand> [flags]\n\nSubcommands:\n  sync      Refresh bound .semsearch state from the latest published remote workflow\n  download  Download one published artifact for a specific commit into local-only state\n  bind      Alias for `bind ci`\n\nUse `%s remote sync --help` or `%s remote download --help` for flags.\n",
		program,
		program,
		program,
	)
	return err
}

func printFlagSetHelp(w io.Writer, fs *flag.FlagSet, usage string, summary string, examples []string, notes []string) error {
	if _, err := fmt.Fprintf(w, "Usage:\n  %s\n", usage); err != nil {
		return err
	}
	if summary != "" {
		if _, err := fmt.Fprintf(w, "\n%s\n", summary); err != nil {
			return err
		}
	}
	if defaults := flagDefaults(fs); defaults != "" {
		if _, err := fmt.Fprintf(w, "\nFlags:\n%s", defaults); err != nil {
			return err
		}
	}
	if len(notes) > 0 {
		if _, err := fmt.Fprintln(w, "\nNotes:"); err != nil {
			return err
		}
		for _, note := range notes {
			if _, err := fmt.Fprintf(w, "  - %s\n", note); err != nil {
				return err
			}
		}
	}
	if len(examples) > 0 {
		if _, err := fmt.Fprintln(w, "\nExamples:"); err != nil {
			return err
		}
		for _, example := range examples {
			if _, err := fmt.Fprintf(w, "  %s\n", example); err != nil {
				return err
			}
		}
	}
	return nil
}

func flagDefaults(fs *flag.FlagSet) string {
	var builder strings.Builder
	originalOutput := fs.Output()
	fs.SetOutput(&builder)
	fs.PrintDefaults()
	fs.SetOutput(originalOutput)
	return builder.String()
}

func cliName(program string) string {
	name := strings.TrimSpace(filepath.Base(program))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "unch"
	}
	return name
}

func isHelpArg(arg string) bool {
	arg = strings.TrimSpace(arg)
	return arg == "-h" || arg == "--help" || arg == "help"
}

func helpWordmark() string {
	wordmark := strings.TrimLeft(rootHelpWordmark, "\n")
	if !isCharDevice(os.Stdout) || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") || os.Getenv("NO_COLOR") != "" {
		return wordmark
	}
	return "\x1b[38;5;48m" + wordmark + "\x1b[0m"
}
