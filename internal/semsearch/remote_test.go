package semsearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitHubWorkflowURL(t *testing.T) {
	t.Parallel()

	got, err := ParseGitHubWorkflowURL("https://github.com/acme/widgets/actions/workflows/searcher.yml")
	if err != nil {
		t.Fatalf("ParseGitHubWorkflowURL() error: %v", err)
	}
	if got.Owner != "acme" || got.Repo != "widgets" || got.WorkflowFile != "searcher.yml" {
		t.Fatalf("ParseGitHubWorkflowURL() = %+v", got)
	}
}

func TestResolveGitHubCIURLFromRepository(t *testing.T) {
	t.Parallel()

	got, err := ResolveGitHubCIURL("https://github.com/acme/widgets")
	if err != nil {
		t.Fatalf("ResolveGitHubCIURL() error: %v", err)
	}
	want := "https://github.com/acme/widgets/actions/workflows/searcher.yml"
	if got != want {
		t.Fatalf("ResolveGitHubCIURL() = %q, want %q", got, want)
	}
}

func TestBindRemoteManifest(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()

	manifest, err := BindRemoteManifest(localDir, "https://github.com/acme/widgets")
	if err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}
	if manifest.Source != "remote" {
		t.Fatalf("manifest.Source = %q, want remote", manifest.Source)
	}
	if manifest.Remote == nil || manifest.Remote.CIURL != "https://github.com/acme/widgets/actions/workflows/searcher.yml" {
		t.Fatalf("manifest.Remote = %+v", manifest.Remote)
	}
}

func TestSyncRemoteIndexDownloadsNewVersion(t *testing.T) {
	localDir := t.TempDir()
	ciURL := "https://github.com/acme/widgets/actions/workflows/searcher.yml"
	if _, err := BindRemoteManifest(localDir, ciURL); err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}

	localDBPath := filepath.Join(localDir, "index.db")
	if err := os.WriteFile(localDBPath, []byte("old-index"), 0o644); err != nil {
		t.Fatalf("write local db: %v", err)
	}
	if err := WriteManifest(localDir, Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       1,
		IndexingHash:  "old-index-hash",
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}); err != nil {
		t.Fatalf("WriteManifest() error: %v", err)
	}

	remoteDBPath := filepath.Join(t.TempDir(), "remote-index.db")
	remoteHash := writeTestIndexDB(t, remoteDBPath, 2, "/tmp/remote.go", 20, "hash2", []float32{3, 2, 1})
	remoteDB := readTestIndexDBBytes(t, remoteDBPath)
	remoteManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       2,
		IndexingHash:  remoteHash,
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/acme/widgets/gh-pages/semsearch/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(remoteManifest)
		case "/acme/widgets/gh-pages/semsearch/index.db":
			_, _ = w.Write(remoteDB)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalBaseURL := gitHubContentBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubContentBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubContentBaseURL = originalBaseURL
		remoteManifestHTTPClient = originalClient
	})

	result, err := SyncRemoteIndex(context.Background(), localDir)
	if err != nil {
		t.Fatalf("SyncRemoteIndex() error: %v", err)
	}
	if !result.Checked {
		t.Fatalf("result.Checked = false, want true")
	}
	if !result.Downloaded {
		t.Fatalf("result.Downloaded = false, want true")
	}

	gotDB, err := os.ReadFile(localDBPath)
	if err != nil {
		t.Fatalf("read synced db: %v", err)
	}
	if string(gotDB) != string(remoteDB) {
		t.Fatalf("synced db = %q, want %q", string(gotDB), string(remoteDB))
	}

	reloaded, err := ReadManifest(localDir)
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if !manifestsEqual(reloaded, remoteManifest) {
		t.Fatalf("ReadManifest() = %+v, want %+v", reloaded, remoteManifest)
	}
}

func TestSyncRemoteIndexFailsWhenRemoteIsMissingAndNoLocalDB(t *testing.T) {
	localDir := t.TempDir()
	ciURL := "https://github.com/acme/widgets/actions/workflows/searcher.yml"
	if _, err := BindRemoteManifest(localDir, ciURL); err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	originalBaseURL := gitHubContentBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubContentBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubContentBaseURL = originalBaseURL
		remoteManifestHTTPClient = originalClient
	})

	_, err := SyncRemoteIndex(context.Background(), localDir)
	if err == nil || !errors.Is(err, ErrRemoteIndexNotPublished) || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("SyncRemoteIndex() error = %v, want ErrRemoteIndexNotPublished", err)
	}
}
