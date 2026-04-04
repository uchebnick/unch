package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSuite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	content := `{
  "suite_id": "default",
  "suite_version": 1,
  "name": "default",
  "repositories": [
    {
      "id": "gorilla/mux",
      "url": "https://github.com/gorilla/mux.git",
      "commit": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
      "language": "go",
      "queries": [
        {
          "id": "new-router",
          "text": "create a new router",
          "mode": "auto",
          "expected_hits": ["mux.go:32"]
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("LoadSuite() error: %v", err)
	}
	if suite.Name != "default" {
		t.Fatalf("LoadSuite() name = %q", suite.Name)
	}
	if suite.ID != "default" || suite.Version != 1 {
		t.Fatalf("LoadSuite() suite = %s v%d", suite.ID, suite.Version)
	}
	if len(suite.Repositories) != 1 {
		t.Fatalf("LoadSuite() repos = %d", len(suite.Repositories))
	}
}

func TestLoadSuiteRejectsMissingRepositoryFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	content := `{
  "suite_id": "default",
  "suite_version": 1,
  "name": "default",
  "repositories": [
    {
      "id": "",
      "url": "https://github.com/gorilla/mux.git",
      "commit": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
      "language": "go",
      "queries": [
        {
          "id": "new-router",
          "text": "create a new router",
          "mode": "auto",
          "expected_hits": ["mux.go:32"]
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	_, err := LoadSuite(path)
	if err == nil || !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("LoadSuite() error = %v, want empty id", err)
	}
}

func TestLoadSuiteRejectsInvalidQueryMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	content := `{
  "suite_id": "default",
  "suite_version": 1,
  "name": "default",
  "repositories": [
    {
      "id": "gorilla/mux",
      "url": "https://github.com/gorilla/mux.git",
      "commit": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
      "language": "go",
      "queries": [
        {
          "id": "new-router",
          "text": "create a new router",
          "mode": "bad-mode",
          "expected_hits": ["mux.go:32"]
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	_, err := LoadSuite(path)
	if err == nil || !strings.Contains(err.Error(), "unknown search mode") {
		t.Fatalf("LoadSuite() error = %v, want invalid mode", err)
	}
}

func TestLoadSuiteRejectsEmptyExpectedHits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	content := `{
  "suite_id": "default",
  "suite_version": 1,
  "name": "default",
  "repositories": [
    {
      "id": "gorilla/mux",
      "url": "https://github.com/gorilla/mux.git",
      "commit": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
      "language": "go",
      "queries": [
        {
          "id": "new-router",
          "text": "create a new router",
          "mode": "auto",
          "expected_hits": []
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	_, err := LoadSuite(path)
	if err == nil || !strings.Contains(err.Error(), "at least one expected hit") {
		t.Fatalf("LoadSuite() error = %v, want empty expected hits", err)
	}
}

func TestLoadSuiteRejectsMissingVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	content := `{
  "suite_id": "default",
  "name": "default",
  "repositories": [
    {
      "id": "gorilla/mux",
      "url": "https://github.com/gorilla/mux.git",
      "commit": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
      "language": "go",
      "queries": [
        {
          "id": "new-router",
          "text": "create a new router",
          "mode": "auto",
          "expected_hits": ["mux.go:32"]
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	_, err := LoadSuite(path)
	if err == nil || !strings.Contains(err.Error(), "suite version") {
		t.Fatalf("LoadSuite() error = %v, want missing suite version", err)
	}
}

func TestCheckedInSuitesLoad(t *testing.T) {
	t.Parallel()

	for _, relPath := range []string{
		filepath.Join("..", "..", "benchmarks", "suites", "ci.json"),
		filepath.Join("..", "..", "benchmarks", "suites", "smoke.json"),
		filepath.Join("..", "..", "benchmarks", "suites", "default.json"),
	} {
		suite, err := LoadSuite(relPath)
		if err != nil {
			t.Fatalf("LoadSuite(%s) error: %v", relPath, err)
		}
		if suite.ID == "" || suite.Version <= 0 {
			t.Fatalf("LoadSuite(%s) = %s v%d", relPath, suite.ID, suite.Version)
		}
	}
}
