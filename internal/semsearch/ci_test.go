package semsearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCIWorkflow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path, created, err := EnsureCIWorkflow(root)
	if err != nil {
		t.Fatalf("EnsureCIWorkflow() error: %v", err)
	}
	if !created {
		t.Fatalf("expected workflow to be created")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}

	content := string(data)
	if content != DefaultCIWorkflow {
		t.Fatalf("workflow content mismatch")
	}
	if !strings.Contains(content, "permissions:\n  contents: write") {
		t.Fatalf("workflow missing write permissions")
	}
	if !strings.Contains(content, "workflow_dispatch:\n    inputs:") {
		t.Fatalf("workflow missing workflow_dispatch inputs")
	}
	if !strings.Contains(content, "force_rebuild:") || !strings.Contains(content, "skip_remote_restore:") || !strings.Contains(content, "skip_publish:") {
		t.Fatalf("workflow missing manual dispatch controls")
	}
	if !strings.Contains(content, "name: unch-index") {
		t.Fatalf("workflow missing unch-index name")
	}
	if !strings.Contains(content, "uses: uchebnick/unch/.github/workflows/searcher-reusable.yml@v0.3.6") {
		t.Fatalf("workflow missing reusable workflow reference")
	}
	if !strings.Contains(content, "unch_repository: uchebnick/unch") {
		t.Fatalf("workflow missing pinned reusable repository")
	}
	if !strings.Contains(content, "unch_ref: v0.3.6") {
		t.Fatalf("workflow missing pinned reusable ref")
	}
	if !strings.Contains(content, "secrets: inherit") {
		t.Fatalf("workflow missing secret inheritance for reusable workflow")
	}
	if strings.Contains(content, "git push origin HEAD:gh-pages") {
		t.Fatalf("delegating workflow should not inline publish logic")
	}
	if strings.Contains(content, "actions/checkout@v4") {
		t.Fatalf("delegating workflow should not inline reusable workflow steps")
	}
	if !strings.Contains(content, ".github/workflows") && !strings.HasSuffix(path, "searcher.yml") {
		t.Fatalf("unexpected workflow path: %s", path)
	}
}

func TestEnsureCIWorkflowDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path, created, err := EnsureCIWorkflow(root)
	if err != nil {
		t.Fatalf("EnsureCIWorkflow() error: %v", err)
	}
	if !created {
		t.Fatalf("expected first call to create workflow")
	}

	const customWorkflow = "name: custom-searcher\n"
	if err := os.WriteFile(path, []byte(customWorkflow), 0o644); err != nil {
		t.Fatalf("write custom workflow: %v", err)
	}

	gotPath, created, err := EnsureCIWorkflow(root)
	if err != nil {
		t.Fatalf("EnsureCIWorkflow(second call) error: %v", err)
	}
	if created {
		t.Fatalf("expected second call not to overwrite workflow")
	}
	if gotPath != path {
		t.Fatalf("EnsureCIWorkflow() path = %q, want %q", gotPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved workflow: %v", err)
	}
	if string(data) != customWorkflow {
		t.Fatalf("workflow was overwritten: %q", string(data))
	}
}

func TestTrackedSearcherWorkflowMatchesLocalWrapper(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", ".github", "workflows", "searcher.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tracked wrapper workflow: %v", err)
	}
	if string(data) != LocalCIWorkflow {
		t.Fatalf("tracked wrapper workflow drifted from LocalCIWorkflow")
	}
}

func TestTrackedSearcherReusableWorkflowMatchesTemplate(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", ".github", "workflows", "searcher-reusable.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tracked reusable workflow: %v", err)
	}
	if string(data) != ReusableCIWorkflow {
		t.Fatalf("tracked reusable workflow drifted from ReusableCIWorkflow")
	}
}
