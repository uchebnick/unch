package semsearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGitignore(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()

	created, err := EnsureGitignore(localDir)
	if err != nil {
		t.Fatalf("EnsureGitignore() error: %v", err)
	}
	if !created {
		t.Fatalf("expected gitignore to be created")
	}

	path := filepath.Join(localDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != DefaultGitignore {
		t.Fatalf(".gitignore = %q, want %q", string(data), DefaultGitignore)
	}

	created, err = EnsureGitignore(localDir)
	if err != nil {
		t.Fatalf("EnsureGitignore(second call) error: %v", err)
	}
	if created {
		t.Fatalf("expected second call not to recreate .gitignore")
	}
}

func TestInitCreatesSemsearchLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", filepath.Join(root, "global"))

	paths, created, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if !created {
		t.Fatalf("expected Init() to create .gitignore")
	}
	if paths.LocalDir != filepath.Join(root, ".semsearch") {
		t.Fatalf("LocalDir = %q", paths.LocalDir)
	}
	if _, err := os.Stat(filepath.Join(paths.LocalDir, ".gitignore")); err != nil {
		t.Fatalf(".gitignore missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LocalDir, "file_weights.json")); err != nil {
		t.Fatalf("file_weights.json missing: %v", err)
	}
	if _, err := os.Stat(paths.ModelsDir); err != nil {
		t.Fatalf("ModelsDir missing: %v", err)
	}
}
