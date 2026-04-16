package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStateTargetDefaultRepoLocalState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	paths, indexPath, stateDirOwnsIndex, err := resolveStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsIndex {
		t.Fatalf("stateDirOwnsIndex = false, want true")
	}
	if paths.LocalDir != filepath.Join(root, ".semsearch") {
		t.Fatalf("paths.LocalDir = %q", paths.LocalDir)
	}
	if indexPath != filepath.Join(root, ".semsearch", "index.db") {
		t.Fatalf("indexPath = %q", indexPath)
	}
}

func TestResolveStateTargetExplicitStateDirKeepsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), ".semsearch")

	paths, indexPath, stateDirOwnsIndex, err := resolveStateTarget(root, stateDir, true, "", false)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsIndex {
		t.Fatalf("stateDirOwnsIndex = false, want true")
	}
	if paths.LocalDir != stateDir {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, stateDir)
	}
	if indexPath != filepath.Join(stateDir, "index.db") {
		t.Fatalf("indexPath = %q", indexPath)
	}
}

func TestResolveStateTargetExplicitIndexDBKeepsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), ".semsearch", "index.db")

	paths, resolvedIndexPath, stateDirOwnsIndex, err := resolveStateTarget(root, "", false, indexPath, true)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsIndex {
		t.Fatalf("stateDirOwnsIndex = false, want true")
	}
	if paths.LocalDir != filepath.Dir(indexPath) {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, filepath.Dir(indexPath))
	}
	if resolvedIndexPath != indexPath {
		t.Fatalf("resolvedIndexPath = %q, want %q", resolvedIndexPath, indexPath)
	}
}

func TestResolveStateTargetExplicitCustomDBSkipsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	indexPath := filepath.Join(t.TempDir(), "custom", "search.db")

	paths, resolvedIndexPath, stateDirOwnsIndex, err := resolveStateTarget(root, "", false, indexPath, true)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if stateDirOwnsIndex {
		t.Fatalf("stateDirOwnsIndex = true, want false")
	}
	if paths.LocalDir != filepath.Dir(indexPath) {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, filepath.Dir(indexPath))
	}
	if resolvedIndexPath != indexPath {
		t.Fatalf("resolvedIndexPath = %q, want %q", resolvedIndexPath, indexPath)
	}
}

func TestResolveStateTargetRejectsStateDirAndDBTogether(t *testing.T) {
	t.Parallel()

	_, _, _, err := resolveStateTarget(t.TempDir(), "/tmp/.semsearch", true, "/tmp/.semsearch/index.db", true)
	if err == nil || err.Error() != "use either --state-dir or --db, not both" {
		t.Fatalf("resolveStateTarget() error = %v", err)
	}
}

func TestPreviewStateTargetDoesNotCreateDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	paths, indexPath, stateDirOwnsIndex, err := previewStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("previewStateTarget() error: %v", err)
	}
	if !stateDirOwnsIndex {
		t.Fatalf("stateDirOwnsIndex = false, want true")
	}
	if paths.LocalDir != filepath.Join(root, ".semsearch") {
		t.Fatalf("paths.LocalDir = %q", paths.LocalDir)
	}
	if indexPath != filepath.Join(root, ".semsearch", "index.db") {
		t.Fatalf("indexPath = %q", indexPath)
	}
	if _, err := os.Stat(paths.LocalDir); !os.IsNotExist(err) {
		t.Fatalf("previewStateTarget created %q unexpectedly", paths.LocalDir)
	}
}
