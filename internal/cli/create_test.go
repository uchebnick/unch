package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uchebnick/unch/internal/semsearch"
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
	if !strings.Contains(content, "name: unch-index") {
		t.Fatalf("generated workflow missing name: %s", content)
	}
	if !strings.Contains(content, "workflow_dispatch:\n    inputs:") {
		t.Fatalf("generated workflow missing workflow_dispatch inputs: %s", content)
	}
	if !strings.Contains(content, "permissions:\n  contents: write") {
		t.Fatalf("generated workflow missing write permissions for gh-pages publish: %s", content)
	}
	if !strings.Contains(content, "uses: uchebnick/unch/.github/workflows/searcher-reusable.yml@v0.3.6") {
		t.Fatalf("generated workflow missing reusable workflow reference: %s", content)
	}
	if !strings.Contains(content, "unch_repository: uchebnick/unch") {
		t.Fatalf("generated workflow missing pinned reusable repository: %s", content)
	}
	if !strings.Contains(content, "unch_ref: v0.3.6") {
		t.Fatalf("generated workflow missing pinned reusable ref: %s", content)
	}
	if !strings.Contains(content, "secrets: inherit") {
		t.Fatalf("generated workflow missing reusable secret inheritance: %s", content)
	}
	if strings.Contains(content, "git push origin HEAD:gh-pages") {
		t.Fatalf("generated workflow should delegate publish logic to the reusable workflow: %s", content)
	}
	if strings.Contains(content, "unch index --root .") {
		t.Fatalf("generated workflow should not inline the index steps anymore: %s", content)
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
