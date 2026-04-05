package cli

import (
	"path/filepath"
	"testing"
)

func TestResolveStateTargetDefaultRepoLocalState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	paths, dbPath, stateDirOwnsDB, err := resolveStateTarget(root, "", false, "", false)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsDB {
		t.Fatalf("stateDirOwnsDB = false, want true")
	}
	if paths.LocalDir != filepath.Join(root, ".semsearch") {
		t.Fatalf("paths.LocalDir = %q", paths.LocalDir)
	}
	if dbPath != filepath.Join(root, ".semsearch", "index.db") {
		t.Fatalf("dbPath = %q", dbPath)
	}
}

func TestResolveStateTargetExplicitStateDirKeepsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), ".semsearch")

	paths, dbPath, stateDirOwnsDB, err := resolveStateTarget(root, stateDir, true, "", false)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsDB {
		t.Fatalf("stateDirOwnsDB = false, want true")
	}
	if paths.LocalDir != stateDir {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, stateDir)
	}
	if dbPath != filepath.Join(stateDir, "index.db") {
		t.Fatalf("dbPath = %q", dbPath)
	}
}

func TestResolveStateTargetExplicitIndexDBKeepsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), ".semsearch", "index.db")

	paths, resolvedDBPath, stateDirOwnsDB, err := resolveStateTarget(root, "", false, dbPath, true)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if !stateDirOwnsDB {
		t.Fatalf("stateDirOwnsDB = false, want true")
	}
	if paths.LocalDir != filepath.Dir(dbPath) {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, filepath.Dir(dbPath))
	}
	if resolvedDBPath != dbPath {
		t.Fatalf("resolvedDBPath = %q, want %q", resolvedDBPath, dbPath)
	}
}

func TestResolveStateTargetExplicitCustomDBSkipsStateDirSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "custom", "search.db")

	paths, resolvedDBPath, stateDirOwnsDB, err := resolveStateTarget(root, "", false, dbPath, true)
	if err != nil {
		t.Fatalf("resolveStateTarget() error: %v", err)
	}
	if stateDirOwnsDB {
		t.Fatalf("stateDirOwnsDB = true, want false")
	}
	if paths.LocalDir != filepath.Dir(dbPath) {
		t.Fatalf("paths.LocalDir = %q, want %q", paths.LocalDir, filepath.Dir(dbPath))
	}
	if resolvedDBPath != dbPath {
		t.Fatalf("resolvedDBPath = %q, want %q", resolvedDBPath, dbPath)
	}
}

func TestResolveStateTargetRejectsStateDirAndDBTogether(t *testing.T) {
	t.Parallel()

	_, _, _, err := resolveStateTarget(t.TempDir(), "/tmp/.semsearch", true, "/tmp/.semsearch/index.db", true)
	if err == nil || err.Error() != "use either --state-dir or --db, not both" {
		t.Fatalf("resolveStateTarget() error = %v", err)
	}
}
