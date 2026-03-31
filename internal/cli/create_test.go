package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uchebnick/unch-searcher/internal/semsearch"
)

func TestRunCreateRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	err := runCreate(context.Background(), "unch", []string{"weird"}, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for unknown create target")
	}
}

func TestRunCreateCI(t *testing.T) {
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
	if !strings.Contains(content, "permissions:\n  contents: write") {
		t.Fatalf("generated workflow missing write permissions for gh-pages publish: %s", content)
	}
	if !strings.Contains(content, "git clone --depth 1 https://github.com/uchebnick/unch.git") {
		t.Fatalf("generated workflow missing unch source clone step: %s", content)
	}
	if !strings.Contains(content, "export PATH=\"$bin_dir:$PATH\"") {
		t.Fatalf("generated workflow missing immediate PATH export: %s", content)
	}
	if !strings.Contains(content, "unch bind ci --root . \"$ci_url\"") {
		t.Fatalf("generated workflow missing bind ci step: %s", content)
	}
	if !strings.Contains(content, "unch remote sync --root . --allow-missing") {
		t.Fatalf("generated workflow missing remote sync step: %s", content)
	}
	if !strings.Contains(content, "unch index --root .") {
		t.Fatalf("generated workflow missing index step: %s", content)
	}
	if !strings.Contains(content, "unch create ci --root \"$probe_dir\" >/dev/null 2>&1") {
		t.Fatalf("generated workflow missing tooling probe step: %s", content)
	}
	if !strings.Contains(content, "GITHUB_TOKEN: ${{ github.token }}") {
		t.Fatalf("generated workflow missing GitHub token env for indexing: %s", content)
	}
	if !strings.Contains(content, "SEMSEARCH_YZMA_PROCESSOR: cpu") {
		t.Fatalf("generated workflow missing pinned yzma processor for indexing: %s", content)
	}
	if !strings.Contains(content, "SEMSEARCH_YZMA_VERSION: b8581") {
		t.Fatalf("generated workflow missing pinned yzma version for indexing: %s", content)
	}
	if !strings.Contains(content, "if-no-files-found: warn") {
		t.Fatalf("generated workflow missing artifact warning mode: %s", content)
	}
	if !strings.Contains(content, "git push origin HEAD:gh-pages") {
		t.Fatalf("generated workflow missing gh-pages publish step: %s", content)
	}
	if !strings.Contains(content, "GITHUB_STEP_SUMMARY") {
		t.Fatalf("generated workflow missing GitHub summary step: %s", content)
	}

	if _, err := os.Stat(filepath.Join(root, ".semsearch")); !os.IsNotExist(err) {
		t.Fatalf("create ci should not initialize .semsearch, stat err=%v", err)
	}
}

func TestRunBindCI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))
	repoURL := "https://github.com/acme/widgets"
	wantCIURL := "https://github.com/acme/widgets/actions/workflows/searcher.yml"

	output := captureStdout(t, func() {
		if err := runBind(context.Background(), "unch", []string{"ci", "--root", root, repoURL}, root); err != nil {
			t.Fatalf("runBind() error: %v", err)
		}
	})

	if !strings.Contains(output, "Bound ") {
		t.Fatalf("runBind() output = %q, want Bound", output)
	}

	manifest, err := semsearch.ReadManifest(filepath.Join(root, ".semsearch"))
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if manifest.Source != "remote" {
		t.Fatalf("manifest.Source = %q, want remote", manifest.Source)
	}
	if manifest.Remote == nil {
		t.Fatalf("manifest.Remote = nil, want GitHub workflow URL")
	}
	if manifest.Remote.CIURL != wantCIURL {
		t.Fatalf("manifest.Remote.CIURL = %q, want %q", manifest.Remote.CIURL, wantCIURL)
	}
}
