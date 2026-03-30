package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSingleGGUFFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := findSingleGGUFFile(root); err == nil {
		t.Fatalf("expected error when no GGUF files exist")
	}

	one := filepath.Join(root, "model.gguf")
	if err := os.WriteFile(one, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("write GGUF file: %v", err)
	}
	got, err := findSingleGGUFFile(root)
	if err != nil {
		t.Fatalf("findSingleGGUFFile(one) error: %v", err)
	}
	if got != one {
		t.Fatalf("findSingleGGUFFile(one) = %q, want %q", got, one)
	}

	other := filepath.Join(root, "other.gguf")
	if err := os.WriteFile(other, []byte("GGUFother"), 0o644); err != nil {
		t.Fatalf("write second GGUF file: %v", err)
	}
	if _, err := findSingleGGUFFile(root); err == nil {
		t.Fatalf("expected error when multiple GGUF files exist")
	}
}

func TestValidateAndActivateGGUFFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "source.gguf")
	dest := filepath.Join(dir, "dest.gguf")

	if err := os.WriteFile(source, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := validateGGUFFile(source); err != nil {
		t.Fatalf("validateGGUFFile(valid) error: %v", err)
	}

	invalid := filepath.Join(dir, "invalid.gguf")
	if err := os.WriteFile(invalid, []byte("FAIL"), 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}
	if err := validateGGUFFile(invalid); err == nil {
		t.Fatalf("expected invalid GGUF validation to fail")
	}

	if err := activateModelFile(source, dest); err != nil {
		t.Fatalf("activateModelFile() error: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("activated destination missing: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source should be moved away, stat err = %v", err)
	}
}

func TestCleanupModelArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(dest, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("write destination file: %v", err)
	}

	leftovers := []string{
		filepath.Join(dir, "model.gguf.tmp-123"),
		filepath.Join(dir, "model.gguf.activate-456"),
	}
	for _, path := range leftovers {
		if err := os.WriteFile(path, []byte("junk"), 0o644); err != nil {
			t.Fatalf("write leftover %s: %v", path, err)
		}
	}

	cleanupModelArtifacts(dest, nil)

	for _, path := range leftovers {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err = %v", path, err)
		}
	}
}

func TestResolveOrInstallModelPathUsesExistingFilesAndNestedGGUF(t *testing.T) {
	t.Parallel()

	cache := ModelCache{}
	dir := t.TempDir()

	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	got, note, err := cache.ResolveOrInstallModelPath(context.Background(), modelPath, modelPath, false, nil)
	if err != nil {
		t.Fatalf("ResolveOrInstallModelPath(file) error: %v", err)
	}
	if got != modelPath || note != "" {
		t.Fatalf("ResolveOrInstallModelPath(file) = (%q, %q)", got, note)
	}

	modelDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	nested := filepath.Join(modelDir, "inside.gguf")
	if err := os.WriteFile(nested, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("write nested GGUF: %v", err)
	}

	got, note, err = cache.ResolveOrInstallModelPath(context.Background(), modelDir, modelPath, false, nil)
	if err != nil {
		t.Fatalf("ResolveOrInstallModelPath(dir) error: %v", err)
	}
	if got != nested || !strings.Contains(note, "using model file found") {
		t.Fatalf("ResolveOrInstallModelPath(dir) = (%q, %q)", got, note)
	}

	if _, _, err := cache.ResolveOrInstallModelPath(context.Background(), filepath.Join(dir, "missing.gguf"), modelPath, false, nil); err == nil {
		t.Fatalf("expected error for missing explicit model path")
	}
}
