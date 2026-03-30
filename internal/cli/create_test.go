package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCreateRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	err := runCreate(context.Background(), "unch", []string{"weird"}, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for unknown create target")
	}
}

func TestRunCreateCI(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := runCreate(context.Background(), "unch", []string{"ci", "--root", root}, root); err != nil {
		t.Fatalf("runCreate() error: %v", err)
	}

	path := filepath.Join(root, ".github", "workflows", "searcher.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated workflow: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "name: searcher") {
		t.Fatalf("generated workflow missing name: %s", content)
	}
	if !strings.Contains(content, "git clone --depth 1 https://github.com/uchebnick/unch.git") {
		t.Fatalf("generated workflow missing unch source clone step: %s", content)
	}
	if !strings.Contains(content, "export PATH=\"$bin_dir:$PATH\"") {
		t.Fatalf("generated workflow missing immediate PATH export: %s", content)
	}
	if !strings.Contains(content, "unch index --root .") {
		t.Fatalf("generated workflow missing index step: %s", content)
	}
	if !strings.Contains(content, "unch create ci --root \"$probe_dir\" >/dev/null") {
		t.Fatalf("generated workflow missing tooling probe step: %s", content)
	}
	if !strings.Contains(content, "GITHUB_TOKEN: ${{ github.token }}") {
		t.Fatalf("generated workflow missing GitHub token env for indexing: %s", content)
	}
	if !strings.Contains(content, "SEMSEARCH_YZMA_VERSION: b8581") {
		t.Fatalf("generated workflow missing pinned yzma version for indexing: %s", content)
	}
	if !strings.Contains(content, "if-no-files-found: warn") {
		t.Fatalf("generated workflow missing artifact warning mode: %s", content)
	}
	if !strings.Contains(content, "GITHUB_STEP_SUMMARY") {
		t.Fatalf("generated workflow missing GitHub summary step: %s", content)
	}
}
