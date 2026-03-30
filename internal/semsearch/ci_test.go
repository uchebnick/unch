package semsearch

import (
	"os"
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
	if !strings.Contains(content, "GITHUB_STEP_SUMMARY") {
		t.Fatalf("workflow missing GitHub summary step")
	}
	if !strings.Contains(content, "export PATH=\"$bin_dir:$PATH\"") {
		t.Fatalf("workflow missing immediate PATH export")
	}
	if !strings.Contains(content, "GITHUB_TOKEN: ${{ github.token }}") {
		t.Fatalf("workflow missing GitHub token env for indexing")
	}
	if !strings.Contains(content, "SEMSEARCH_YZMA_VERSION: b8581") {
		t.Fatalf("workflow missing pinned yzma version for indexing")
	}
	if !strings.Contains(content, ".semsearch/logs/searcher-index.log") {
		t.Fatalf("workflow missing explicit searcher log output")
	}
	if !strings.Contains(content, "if-no-files-found: warn") {
		t.Fatalf("workflow missing artifact warning mode")
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
