package semsearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFileWeights(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()

	created, err := EnsureFileWeights(localDir)
	if err != nil {
		t.Fatalf("EnsureFileWeights() error: %v", err)
	}
	if !created {
		t.Fatalf("expected file_weights.json to be created")
	}

	path := FileWeightsPath(localDir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file_weights.json: %v", err)
	}
	if string(data) != DefaultFileWeightsJSON {
		t.Fatalf("file_weights.json = %q, want default config", string(data))
	}

	created, err = EnsureFileWeights(localDir)
	if err != nil {
		t.Fatalf("EnsureFileWeights(second call) error: %v", err)
	}
	if created {
		t.Fatalf("expected second call not to recreate file_weights.json")
	}
}

func TestLoadFileWeightsAppliesMinimumMatchingWeight(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	content := `{
  "**/*_test.*": 0.82,
  "**/examples/**": 0.88,
  "**/vendor/**": 0.72
}
`
	if err := os.WriteFile(FileWeightsPath(localDir), []byte(content), 0o644); err != nil {
		t.Fatalf("write file_weights.json: %v", err)
	}

	weights, err := LoadFileWeights(localDir)
	if err != nil {
		t.Fatalf("LoadFileWeights() error: %v", err)
	}

	if got := weights.Weight("pkg/service.go"); got != 1 {
		t.Fatalf("Weight(non-matching) = %v, want 1", got)
	}
	if got := weights.Weight("pkg/service_test.go"); got != 0.82 {
		t.Fatalf("Weight(test file) = %v, want 0.82", got)
	}
	if got := weights.Weight(filepath.ToSlash("./examples/demo_test.go")); got != 0.82 {
		t.Fatalf("Weight(multi-match) = %v, want min 0.82", got)
	}
	if got := weights.Weight("vendor/acme/lib.go"); got != 0.72 {
		t.Fatalf("Weight(vendor file) = %v, want 0.72", got)
	}
}

func TestLoadFileWeightsRejectsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	if err := os.WriteFile(FileWeightsPath(localDir), []byte(`{"**/*_test.*": 1.2}`), 0o644); err != nil {
		t.Fatalf("write invalid file_weights.json: %v", err)
	}

	if _, err := LoadFileWeights(localDir); err == nil {
		t.Fatalf("expected LoadFileWeights() to reject out-of-range weight")
	}
}
