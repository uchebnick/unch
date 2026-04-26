package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	unchmcp "github.com/uchebnick/unch/internal/mcp"
	"github.com/uchebnick/unch/internal/semsearch"
)

func TestMCPBackendWorkspaceStatusUsesDirectoryArgument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	other := t.TempDir()

	paths, indexPath, _, err := previewStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("previewStateTarget() error: %v", err)
	}
	backend := newMCPBackend(mcpBackendConfig{
		RootAbs:           root,
		TargetPaths:       paths,
		IndexPath:         indexPath,
		RequestedProvider: "llama.cpp",
	})

	status, err := backend.WorkspaceStatus(context.Background(), unchmcp.WorkspaceStatusParams{
		Directory: other,
	})
	if err != nil {
		t.Fatalf("WorkspaceStatus() error: %v", err)
	}

	if status.Root != other {
		t.Fatalf("Root = %q, want %q", status.Root, other)
	}
	if want := filepath.Join(other, ".semsearch"); status.StateDir != want {
		t.Fatalf("StateDir = %q, want %q", status.StateDir, want)
	}
	if _, err := os.Stat(filepath.Join(other, ".semsearch")); !os.IsNotExist(err) {
		t.Fatalf("workspace_status created state dir unexpectedly: %v", err)
	}
}

func TestMCPBackendWorkspaceStatusRejectsFileDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths, indexPath, _, err := previewStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("previewStateTarget() error: %v", err)
	}
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error: %v", filePath, err)
	}

	backend := newMCPBackend(mcpBackendConfig{
		RootAbs:           root,
		TargetPaths:       paths,
		IndexPath:         indexPath,
		RequestedProvider: "llama.cpp",
	})

	if _, err := backend.WorkspaceStatus(context.Background(), unchmcp.WorkspaceStatusParams{
		Directory: filePath,
	}); err == nil {
		t.Fatalf("WorkspaceStatus() error = nil, want file directory error")
	}
}

func TestMCPBackendCreateCIWorkflowUsesDirectoryArgument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	other := t.TempDir()
	backend := testMCPBackend(t, root)

	result, err := backend.CreateCIWorkflow(context.Background(), unchmcp.CreateCIWorkflowParams{
		Directory: other,
	})
	if err != nil {
		t.Fatalf("CreateCIWorkflow() error: %v", err)
	}
	if result.Root != other {
		t.Fatalf("Root = %q, want %q", result.Root, other)
	}
	if _, err := os.Stat(filepath.Join(other, ".github", "workflows", "unch-index.yml")); err != nil {
		t.Fatalf("expected workflow in directory argument repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "unch-index.yml")); !os.IsNotExist(err) {
		t.Fatalf("workflow exists unexpectedly in launch root")
	}
}

func TestMCPBackendBindRemoteCI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))
	backend := testMCPBackend(t, root)

	result, err := backend.BindRemoteCI(context.Background(), unchmcp.BindRemoteCIParams{
		Target: "https://github.com/acme/widgets",
	})
	if err != nil {
		t.Fatalf("BindRemoteCI() error: %v", err)
	}
	wantCIURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
	if result.CIURL != wantCIURL {
		t.Fatalf("CIURL = %q, want %q", result.CIURL, wantCIURL)
	}
	manifest, err := semsearch.ReadManifest(filepath.Join(root, ".semsearch"))
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if manifest.Source != "remote" {
		t.Fatalf("manifest.Source = %q, want remote", manifest.Source)
	}
}

func TestMCPBackendRemoteSyncIndexUnbound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backend := testMCPBackend(t, root)

	result, err := backend.RemoteSyncIndex(context.Background(), unchmcp.RemoteSyncIndexParams{})
	if err != nil {
		t.Fatalf("RemoteSyncIndex() error: %v", err)
	}
	if result.Checked {
		t.Fatalf("Checked = true, want false for unbound workspace")
	}
	if result.StateDir != filepath.Join(root, ".semsearch") {
		t.Fatalf("StateDir = %q, want root .semsearch", result.StateDir)
	}
}

func testMCPBackend(t *testing.T, root string) *mcpBackend {
	t.Helper()

	paths, indexPath, _, err := previewStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("previewStateTarget() error: %v", err)
	}
	return newMCPBackend(mcpBackendConfig{
		RootAbs:           root,
		TargetPaths:       paths,
		IndexPath:         indexPath,
		RequestedProvider: "llama.cpp",
	})
}
