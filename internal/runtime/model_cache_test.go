package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestResolveOrInstallModelPathAutoDownloadsKnownModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "GGUFpayload")
	}))
	defer server.Close()

	t.Setenv("SEMSEARCH_MODEL_URL", server.URL+"/embeddinggemma.gguf")

	cache := ModelCache{}
	defaultPath := DefaultModelPath(t.TempDir())

	got, note, err := cache.ResolveOrInstallModelPath(context.Background(), "", defaultPath, true, nil)
	if err != nil {
		t.Fatalf("ResolveOrInstallModelPath(auto-download) error = %v", err)
	}
	if got != defaultPath {
		t.Fatalf("ResolveOrInstallModelPath(auto-download) path = %q, want %q", got, defaultPath)
	}
	if !strings.Contains(note, "downloaded embeddinggemma-300m model") {
		t.Fatalf("ResolveOrInstallModelPath(auto-download) note = %q", note)
	}

	data, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("ReadFile(downloaded model) error = %v", err)
	}
	if got := string(data); got != "GGUFpayload" {
		t.Fatalf("downloaded model contents = %q, want %q", got, "GGUFpayload")
	}
}

func TestRepairInstalledModelRepairsNestedGGUF(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "embeddinggemma-300m.gguf")
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(model dir) error = %v", err)
	}

	nested := filepath.Join(destPath, "nested.gguf")
	if err := os.WriteFile(nested, []byte("GGUFpayload"), 0o644); err != nil {
		t.Fatalf("WriteFile(nested model) error = %v", err)
	}

	note, err := repairInstalledModel(destPath, nil)
	if err != nil {
		t.Fatalf("repairInstalledModel() error = %v", err)
	}
	if !strings.Contains(note, "repaired cached model") {
		t.Fatalf("repairInstalledModel() note = %q", note)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Stat(repaired path) error = %v", err)
	}
	if info.IsDir() {
		t.Fatalf("repairInstalledModel() left %s as a directory", destPath)
	}
}

func TestResolveModelSelectionSupportsKnownAliases(t *testing.T) {
	t.Parallel()

	defaultPath := filepath.Join(t.TempDir(), "embeddinggemma-300m.gguf")

	selection, err := resolveModelSelection("", defaultPath)
	if err != nil {
		t.Fatalf("resolveModelSelection(default) error: %v", err)
	}
	if got, want := selection.ResolvedPath, defaultPath; got != want {
		t.Fatalf("resolveModelSelection(default) path = %q, want %q", got, want)
	}
	if !selection.AutoDownload || selection.Target.ID != "embeddinggemma" {
		t.Fatalf("resolveModelSelection(default) = %#v", selection)
	}

	qwenSelection, err := resolveModelSelection("qwen3", defaultPath)
	if err != nil {
		t.Fatalf("resolveModelSelection(qwen3) error: %v", err)
	}
	wantQwen := filepath.Join(filepath.Dir(defaultPath), "Qwen3-Embedding-0.6B-Q8_0.gguf")
	if got := qwenSelection.ResolvedPath; got != wantQwen {
		t.Fatalf("resolveModelSelection(qwen3) path = %q, want %q", got, wantQwen)
	}
	if !qwenSelection.AutoDownload || qwenSelection.Target.ID != "qwen3" {
		t.Fatalf("resolveModelSelection(qwen3) = %#v", qwenSelection)
	}
}

func TestCanonicalModelPathSupportsAliases(t *testing.T) {
	t.Parallel()

	modelsDir := t.TempDir()
	defaultPath := DefaultModelPath(modelsDir)

	got, err := CanonicalModelPath("embeddinggemma", defaultPath)
	if err != nil {
		t.Fatalf("CanonicalModelPath(embeddinggemma) error: %v", err)
	}
	if got != defaultPath {
		t.Fatalf("CanonicalModelPath(embeddinggemma) = %q, want %q", got, defaultPath)
	}

	got, err = CanonicalModelPath("qwen3", defaultPath)
	if err != nil {
		t.Fatalf("CanonicalModelPath(qwen3) error: %v", err)
	}
	want := filepath.Join(modelsDir, "Qwen3-Embedding-0.6B-Q8_0.gguf")
	if got != want {
		t.Fatalf("CanonicalModelPath(qwen3) = %q, want %q", got, want)
	}
}

func TestCanonicalModelIDSupportsAliasesAndCustomPaths(t *testing.T) {
	t.Parallel()

	modelsDir := t.TempDir()
	defaultPath := DefaultModelPath(modelsDir)

	got, err := CanonicalModelID("embeddinggemma", defaultPath)
	if err != nil {
		t.Fatalf("CanonicalModelID(embeddinggemma) error: %v", err)
	}
	if got != "embeddinggemma" {
		t.Fatalf("CanonicalModelID(embeddinggemma) = %q", got)
	}

	got, err = CanonicalModelID("qwen3", defaultPath)
	if err != nil {
		t.Fatalf("CanonicalModelID(qwen3) error: %v", err)
	}
	if got != "qwen3" {
		t.Fatalf("CanonicalModelID(qwen3) = %q", got)
	}

	customPath := filepath.Join(modelsDir, "custom.gguf")
	got, err = CanonicalModelID(customPath, defaultPath)
	if err != nil {
		t.Fatalf("CanonicalModelID(custom) error: %v", err)
	}
	want := "custom:" + filepath.ToSlash(customPath)
	if got != want {
		t.Fatalf("CanonicalModelID(custom) = %q, want %q", got, want)
	}
}
