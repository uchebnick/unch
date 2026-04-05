package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/runtime"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(data)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = originalStderr
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(data)
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})
}

func TestRunCreateRequiresTarget(t *testing.T) {
	t.Parallel()

	err := runCreate(context.Background(), "unch", nil, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for missing create target")
	}
}

func TestRunCreateCIAlreadyExists(t *testing.T) {
	root := t.TempDir()

	first := captureStdout(t, func() {
		if err := runCreate(context.Background(), "unch", []string{"ci", "--root", root}, root); err != nil {
			t.Fatalf("first runCreate() error: %v", err)
		}
	})
	if !strings.Contains(first, "Created ") {
		t.Fatalf("first runCreate() output = %q, want Created", first)
	}

	second := captureStdout(t, func() {
		if err := runCreate(context.Background(), "unch", []string{"ci", "--root", root}, root); err != nil {
			t.Fatalf("second runCreate() error: %v", err)
		}
	})
	if !strings.Contains(second, "Already exists ") {
		t.Fatalf("second runCreate() output = %q, want Already exists", second)
	}
}

func TestRunInitCreatesSemsearchLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), "unch", []string{"--root", root}, root); err != nil {
			t.Fatalf("runInit() error: %v", err)
		}
	})

	if !strings.Contains(output, "Initialized ") {
		t.Fatalf("runInit() output = %q, want Initialized", output)
	}
	if _, err := os.Stat(filepath.Join(root, ".semsearch", ".gitignore")); err != nil {
		t.Fatalf("expected .gitignore to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".semsearch", "manifest.json")); err != nil {
		t.Fatalf("expected manifest.json to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".semsearch", "file_weights.json")); err != nil {
		t.Fatalf("expected file_weights.json to be created: %v", err)
	}
}

func TestRunDispatchesInitCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))
	chdirForTest(t, root)

	output := captureStdout(t, func() {
		if err := Run("unch", []string{"init"}); err != nil {
			t.Fatalf("Run(init) error: %v", err)
		}
	})

	if !strings.Contains(output, "Initialized ") {
		t.Fatalf("Run(init) output = %q, want Initialized", output)
	}
}

func TestRunDispatchesCreateCommand(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)

	output := captureStdout(t, func() {
		if err := Run("unch", []string{"create", "ci"}); err != nil {
			t.Fatalf("Run(create ci) error: %v", err)
		}
	})

	if !strings.Contains(output, "Created ") {
		t.Fatalf("Run(create ci) output = %q, want Created", output)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "searcher.yml")); err != nil {
		t.Fatalf("expected searcher workflow to be created: %v", err)
	}
}

func TestRunDispatchesRemoteSyncCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))
	chdirForTest(t, root)

	output := captureStdout(t, func() {
		if err := Run("unch", []string{"remote", "sync"}); err != nil {
			t.Fatalf("Run(remote sync) error: %v", err)
		}
	})

	if !strings.Contains(output, "not bound") {
		t.Fatalf("Run(remote sync) output = %q, want not bound", output)
	}
}

func TestRunRemoteDownloadRequiresCommit(t *testing.T) {
	t.Parallel()

	err := runRemote(context.Background(), "unch", []string{"download", "https://github.com/acme/widgets"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "requires --commit") {
		t.Fatalf("runRemote(download) error = %v, want missing commit error", err)
	}
}

func TestRunDispatchesBindCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))
	chdirForTest(t, root)

	output := captureStdout(t, func() {
		if err := Run("unch", []string{"bind", "ci", "https://github.com/acme/widgets/actions/workflows/searcher.yml"}); err != nil {
			t.Fatalf("Run(bind ci) error: %v", err)
		}
	})

	if !strings.Contains(output, "Bound ") {
		t.Fatalf("Run(bind ci) output = %q, want Bound", output)
	}
}

func TestRunRootHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Run("unch", []string{"--help"}); err != nil {
			t.Fatalf("Run(--help) error: %v", err)
		}
	})

	if !strings.Contains(output, "Semantic code search for code symbols and docs.") {
		t.Fatalf("Run(--help) output = %q, want root summary", output)
	}
	if !strings.Contains(output, "Model selection:") {
		t.Fatalf("Run(--help) output = %q, want model selection section", output)
	}
	if !strings.Contains(output, "unch help search") {
		t.Fatalf("Run(--help) output = %q, want help example", output)
	}
}

func TestRunSearchHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Run("unch", []string{"search", "--help"}); err != nil {
			t.Fatalf("Run(search --help) error: %v", err)
		}
	})

	if !strings.Contains(output, "Search the current index using semantic, lexical, or mixed retrieval.") {
		t.Fatalf("Run(search --help) output = %q, want search summary", output)
	}
	if !strings.Contains(output, "--details") {
		t.Fatalf("Run(search --help) output = %q, want --details flag", output)
	}
	if !strings.Contains(output, "Use the same embedding model for both index and search") {
		t.Fatalf("Run(search --help) output = %q, want model note", output)
	}
}

func TestRunRemoteHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Run("unch", []string{"remote", "--help"}); err != nil {
			t.Fatalf("Run(remote --help) error: %v", err)
		}
	})

	if !strings.Contains(output, "remote sync") {
		t.Fatalf("Run(remote --help) output = %q, want sync subcommand", output)
	}
	if !strings.Contains(output, "remote download") {
		t.Fatalf("Run(remote --help) output = %q, want download subcommand", output)
	}
	if !strings.Contains(output, "specific commit") {
		t.Fatalf("Run(remote --help) output = %q, want commit-oriented download guidance", output)
	}
}

func TestRunSearchRequiresQuery(t *testing.T) {
	t.Parallel()

	err := runSearch(context.Background(), "unch", nil, ".", indexing.FileScanner{}, runtime.YzmaResolver{}, runtime.ModelCache{})
	if err == nil || !strings.Contains(err.Error(), "empty search query") {
		t.Fatalf("runSearch() error = %v, want empty search query", err)
	}
}

func TestRunSearchRejectsInvalidMode(t *testing.T) {
	t.Parallel()

	err := runSearch(context.Background(), "unch", []string{"--mode", "weird", "--query", "abc"}, ".", indexing.FileScanner{}, runtime.YzmaResolver{}, runtime.ModelCache{})
	if err == nil || !strings.Contains(err.Error(), "unknown search mode") {
		t.Fatalf("runSearch() error = %v, want unknown search mode", err)
	}
}

func TestRunIndexRejectsUnknownFlag(t *testing.T) {
	stderr := captureStderr(t, func() {
		err := runIndex(context.Background(), "unch", []string{"--wat"}, ".", indexing.FileScanner{}, runtime.YzmaResolver{}, runtime.ModelCache{})
		if err == nil {
			t.Fatalf("expected runIndex() to reject unknown flag")
		}
	})

	if !strings.Contains(stderr, "flag provided but not defined: -wat") {
		t.Fatalf("runIndex() stderr = %q, want unknown flag message", stderr)
	}
}

func TestConfirmRemoteReindexAcceptsYes(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	confirmed, err := confirmRemoteReindex(nil, strings.NewReader("yes\n"), &output, true)
	if err != nil {
		t.Fatalf("confirmRemoteReindex() error: %v", err)
	}
	if !confirmed {
		t.Fatalf("confirmRemoteReindex() = false, want true")
	}
	if !strings.Contains(output.String(), "[yes/no]") {
		t.Fatalf("confirmRemoteReindex() output = %q, want prompt", output.String())
	}
}

func TestConfirmRemoteReindexRejectsNo(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	confirmed, err := confirmRemoteReindex(nil, strings.NewReader("no\n"), &output, true)
	if err != nil {
		t.Fatalf("confirmRemoteReindex() error: %v", err)
	}
	if confirmed {
		t.Fatalf("confirmRemoteReindex() = true, want false")
	}
}

func TestConfirmRemoteReindexRepromptsOnInvalidAnswer(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	confirmed, err := confirmRemoteReindex(nil, strings.NewReader("maybe\nyes\n"), &output, true)
	if err != nil {
		t.Fatalf("confirmRemoteReindex() error: %v", err)
	}
	if !confirmed {
		t.Fatalf("confirmRemoteReindex() = false, want true")
	}
	if !strings.Contains(output.String(), "Please answer yes or no.") {
		t.Fatalf("confirmRemoteReindex() output = %q, want retry hint", output.String())
	}
}

func TestStringListFlag(t *testing.T) {
	t.Parallel()

	var values stringListFlag
	if err := values.Set("one"); err != nil {
		t.Fatalf("Set(one) error: %v", err)
	}
	if err := values.Set("two"); err != nil {
		t.Fatalf("Set(two) error: %v", err)
	}
	if got := values.String(); got != "one,two" {
		t.Fatalf("String() = %q, want %q", got, "one,two")
	}
}

func TestDefaultPooling(t *testing.T) {
	t.Parallel()

	if got := defaultPooling("/tmp/embeddinggemma-300m.gguf"); got != llama.PoolingTypeMean {
		t.Fatalf("defaultPooling(gemma) = %v, want %v", got, llama.PoolingTypeMean)
	}
	if got := defaultPooling("/tmp/Qwen3-Embedding-0.6B-Q8_0.gguf"); got != llama.PoolingTypeLast {
		t.Fatalf("defaultPooling(qwen3) = %v, want %v", got, llama.PoolingTypeLast)
	}
}
