package filehashdb

import (
	"context"
	"path/filepath"
	"testing"
)

const testProvider = "llama.cpp"

func TestStoreStageActivateAndCurrent(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	version, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v1", map[string]string{
		"a.go": "aaa",
		"b.go": "bbb",
	})
	if err != nil {
		t.Fatalf("StageState() error: %v", err)
	}
	if version != 1 {
		t.Fatalf("StageState() version = %d, want 1", version)
	}

	if _, ok, err := store.Current(context.Background(), testProvider, "embeddinggemma"); err != nil {
		t.Fatalf("Current(before activate) error: %v", err)
	} else if ok {
		t.Fatalf("Current(before activate) ok = true, want false")
	}

	if err := store.ActivateState(context.Background(), testProvider, "embeddinggemma", version); err != nil {
		t.Fatalf("ActivateState() error: %v", err)
	}

	state, ok, err := store.Current(context.Background(), testProvider, "embeddinggemma")
	if err != nil {
		t.Fatalf("Current() error: %v", err)
	}
	if !ok {
		t.Fatalf("Current() ok = false, want true")
	}
	if state.Version != 1 {
		t.Fatalf("Current() version = %d, want 1", state.Version)
	}
	if state.ScannerFingerprint != "scan-v1" {
		t.Fatalf("Current() fingerprint = %q", state.ScannerFingerprint)
	}
	if len(state.Files) != 2 || state.Files["a.go"] != "aaa" || state.Files["b.go"] != "bbb" {
		t.Fatalf("Current() files = %#v", state.Files)
	}
}

func TestStoreMatchesAndCleanupInactiveStates(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	firstVersion, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v1", map[string]string{
		"a.go": "aaa",
	})
	if err != nil {
		t.Fatalf("first StageState() error: %v", err)
	}
	if err := store.ActivateState(context.Background(), testProvider, "embeddinggemma", firstVersion); err != nil {
		t.Fatalf("ActivateState(first) error: %v", err)
	}

	match, version, err := store.Matches(context.Background(), testProvider, "embeddinggemma", "scan-v1", map[string]string{
		"a.go": "aaa",
	})
	if err != nil {
		t.Fatalf("Matches() error: %v", err)
	}
	if !match || version != 1 {
		t.Fatalf("Matches() = (%v, %d), want (true, 1)", match, version)
	}

	match, version, err = store.Matches(context.Background(), testProvider, "embeddinggemma", "scan-v2", map[string]string{
		"a.go": "aaa",
	})
	if err != nil {
		t.Fatalf("Matches(fingerprint mismatch) error: %v", err)
	}
	if match || version != 1 {
		t.Fatalf("Matches(fingerprint mismatch) = (%v, %d), want (false, 1)", match, version)
	}

	secondVersion, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v2", map[string]string{
		"a.go": "aaa",
		"b.go": "bbb",
	})
	if err != nil {
		t.Fatalf("second StageState() error: %v", err)
	}
	if secondVersion != 2 {
		t.Fatalf("second StageState() version = %d, want 2", secondVersion)
	}

	if err := store.ActivateState(context.Background(), testProvider, "embeddinggemma", secondVersion); err != nil {
		t.Fatalf("ActivateState(second) error: %v", err)
	}
	if err := store.CleanupInactiveStates(context.Background()); err != nil {
		t.Fatalf("CleanupInactiveStates(after activate) error: %v", err)
	}

	state, ok, err := store.Current(context.Background(), testProvider, "embeddinggemma")
	if err != nil {
		t.Fatalf("Current() error: %v", err)
	}
	if !ok {
		t.Fatalf("Current() ok = false, want true")
	}
	if state.Version != 2 || state.ScannerFingerprint != "scan-v2" {
		t.Fatalf("Current() = %+v", state)
	}
	if len(state.Files) != 2 || state.Files["b.go"] != "bbb" {
		t.Fatalf("Current() files = %#v", state.Files)
	}
}

func TestCleanupInactiveStatesDropsUnactivatedState(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	firstVersion, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v1", map[string]string{
		"a.go": "aaa",
	})
	if err != nil {
		t.Fatalf("first StageState() error: %v", err)
	}
	if err := store.ActivateState(context.Background(), testProvider, "embeddinggemma", firstVersion); err != nil {
		t.Fatalf("ActivateState(first) error: %v", err)
	}

	secondVersion, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v2", map[string]string{
		"a.go": "aaa",
		"b.go": "bbb",
	})
	if err != nil {
		t.Fatalf("second StageState() error: %v", err)
	}

	if err := store.CleanupInactiveStates(context.Background()); err != nil {
		t.Fatalf("CleanupInactiveStates() error: %v", err)
	}

	if err := store.ActivateState(context.Background(), testProvider, "embeddinggemma", secondVersion); err == nil {
		t.Fatalf("ActivateState() succeeded for cleaned-up inactive state")
	}

	state, ok, err := store.Current(context.Background(), testProvider, "embeddinggemma")
	if err != nil {
		t.Fatalf("Current() error: %v", err)
	}
	if !ok || state.Version != firstVersion {
		t.Fatalf("Current() = (%+v, %v), want version %d", state, ok, firstVersion)
	}
}

func TestActivateStateRejectsWrongModel(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	version, err := store.StageState(context.Background(), testProvider, "embeddinggemma", "scan-v1", map[string]string{"a.go": "aaa"})
	if err != nil {
		t.Fatalf("StageState() error: %v", err)
	}

	if err := store.ActivateState(context.Background(), testProvider, "qwen3", version); err == nil {
		t.Fatalf("ActivateState() succeeded with wrong model")
	}
}
