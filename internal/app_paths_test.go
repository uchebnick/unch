package internal

import (
	"path/filepath"
	"testing"
)

func TestGlobalSemsearchDirUsesEnvOverride(t *testing.T) {
	t.Setenv("SEMSEARCH_HOME", "/tmp/custom-semsearch-home")

	got, err := globalSemsearchDir()
	if err != nil {
		t.Fatalf("globalSemsearchDir returned error: %v", err)
	}

	if got != filepath.Clean("/tmp/custom-semsearch-home") {
		t.Fatalf("globalSemsearchDir() = %q, want %q", got, filepath.Clean("/tmp/custom-semsearch-home"))
	}
}
