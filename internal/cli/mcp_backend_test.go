package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	unchmcp "github.com/uchebnick/unch/internal/mcp"
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
