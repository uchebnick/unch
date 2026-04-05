package semsearch

import (
	"archive/zip"
	"bytes"
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

	got, err := ParseGitHubWorkflowURL("https://github.com/acme/widgets/actions/workflows/unch-index.yml")
	if err != nil {
		t.Fatalf("ParseGitHubWorkflowURL() error: %v", err)
	}
	if got.Owner != "acme" || got.Repo != "widgets" || got.WorkflowFile != "unch-index.yml" {
		t.Fatalf("ParseGitHubWorkflowURL() = %+v", got)
	}
}

func TestResolveGitHubCIURLFromRepository(t *testing.T) {
	t.Parallel()

	got, err := ResolveGitHubCIURL("https://github.com/acme/widgets")
	if err != nil {
		t.Fatalf("ResolveGitHubCIURL() error: %v", err)
	}
	want := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
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
	if manifest.Remote == nil || manifest.Remote.CIURL != "https://github.com/acme/widgets/actions/workflows/unch-index.yml" {
		t.Fatalf("manifest.Remote = %+v", manifest.Remote)
	}
}

func TestSyncRemoteIndexDownloadsNewVersion(t *testing.T) {
	localDir := t.TempDir()
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
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
	remoteFileHashes := []byte("remote-file-hash-cache\n")
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
		case "/acme/widgets/gh-pages/semsearch/filehashes.db":
			_, _ = w.Write(remoteFileHashes)
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
	gotFileHashes, err := os.ReadFile(filepath.Join(localDir, "filehashes.db"))
	if err != nil {
		t.Fatalf("read synced file hash cache: %v", err)
	}
	if string(gotFileHashes) != string(remoteFileHashes) {
		t.Fatalf("synced filehashes.db = %q, want %q", string(gotFileHashes), string(remoteFileHashes))
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
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
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

func TestSyncRemoteIndexFailsWhenRemoteSchemaIsIncompatibleAndNoLocalDB(t *testing.T) {
	localDir := t.TempDir()
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
	if _, err := BindRemoteManifest(localDir, ciURL); err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}

	legacyDBPath := filepath.Join(t.TempDir(), "legacy-index.db")
	writeLegacyTestIndexDB(t, legacyDBPath, 7)
	legacyDB := readTestIndexDBBytes(t, legacyDBPath)
	remoteManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       7,
		IndexingHash:  "legacy-hash",
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/acme/widgets/gh-pages/semsearch/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(remoteManifest)
		case "/acme/widgets/gh-pages/semsearch/index.db":
			_, _ = w.Write(legacyDB)
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

	_, err := SyncRemoteIndex(context.Background(), localDir)
	if err == nil || !errors.Is(err, ErrRemoteIndexIncompatible) {
		t.Fatalf("SyncRemoteIndex() error = %v, want ErrRemoteIndexIncompatible", err)
	}

	reloaded, readErr := ReadManifest(localDir)
	if readErr != nil {
		t.Fatalf("ReadManifest() error: %v", readErr)
	}
	if reloaded.Version != 7 {
		t.Fatalf("ReadManifest().Version = %d, want 7", reloaded.Version)
	}
	if fileExists(filepath.Join(localDir, "index.db")) {
		t.Fatalf("expected no local index.db to be activated after incompatible remote schema")
	}
}

func TestSyncRemoteIndexSeedsNextCIVersion(t *testing.T) {
	localDir := t.TempDir()
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"
	if _, err := BindRemoteManifest(localDir, ciURL); err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}

	remoteDBPath := filepath.Join(t.TempDir(), "remote-index.db")
	remoteHash := writeTestIndexDB(t, remoteDBPath, 7, "/tmp/remote.go", 20, "hash7", []float32{7, 7, 7})
	remoteDB := readTestIndexDBBytes(t, remoteDBPath)
	remoteManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       7,
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

	if _, err := SyncRemoteIndex(context.Background(), localDir); err != nil {
		t.Fatalf("SyncRemoteIndex() error: %v", err)
	}

	dbPath := filepath.Join(localDir, "index.db")
	manifestAfterIndex, err := UpdateIndexManifest(localDir, dbPath, 123)
	if err != nil {
		t.Fatalf("UpdateIndexManifest() error: %v", err)
	}
	if manifestAfterIndex.Version != 8 {
		t.Fatalf("manifestAfterIndex.Version = %d, want 8", manifestAfterIndex.Version)
	}
	if manifestAfterIndex.Source != "local" {
		t.Fatalf("manifestAfterIndex.Source = %q, want local", manifestAfterIndex.Source)
	}

	manifestAfterBind, err := BindRemoteManifest(localDir, ciURL)
	if err != nil {
		t.Fatalf("BindRemoteManifest(second call) error: %v", err)
	}
	if manifestAfterBind.Version != 8 {
		t.Fatalf("manifestAfterBind.Version = %d, want 8", manifestAfterBind.Version)
	}
	if manifestAfterBind.Source != "remote" {
		t.Fatalf("manifestAfterBind.Source = %q, want remote", manifestAfterBind.Source)
	}
	if manifestAfterBind.Remote == nil || manifestAfterBind.Remote.CIURL != ciURL {
		t.Fatalf("manifestAfterBind.Remote = %+v", manifestAfterBind.Remote)
	}
}

func TestDownloadIndexArtifactForCommit(t *testing.T) {
	localDir := t.TempDir()
	commitSHA := "8dfac53123456789abcdef1234567890abcdef12"
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"

	remoteDBPath := filepath.Join(t.TempDir(), "artifact-index.db")
	remoteHash := writeTestIndexDB(t, remoteDBPath, 9, "internal/search/service.go", 42, "hash9", []float32{9, 4, 2})
	remoteDB := readTestIndexDBBytes(t, remoteDBPath)
	remoteFileHashes := []byte("artifact-file-hash-cache\n")
	artifactManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       9,
		IndexingHash:  remoteHash,
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}
	artifactZip := buildArtifactArchiveWithExtraFiles(t, artifactManifest, remoteDB, map[string][]byte{
		"filehashes.db": remoteFileHashes,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widgets/actions/workflows/unch-index.yml/runs":
			if got := r.URL.Query().Get("head_sha"); got != commitSHA {
				t.Fatalf("runs head_sha = %q, want %q", got, commitSHA)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{
					{
						"id":         123,
						"head_sha":   commitSHA,
						"status":     "completed",
						"conclusion": "success",
					},
				},
			})
		case "/repos/acme/widgets/actions/runs/123/artifacts":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifacts": []map[string]any{
					{
						"id":      456,
						"name":    searchIndexArtifactName,
						"expired": false,
					},
				},
			})
		case "/repos/acme/widgets/actions/artifacts/456/zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(artifactZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPIBaseURL := gitHubAPIBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubAPIBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubAPIBaseURL = originalAPIBaseURL
		remoteManifestHTTPClient = originalClient
	})

	result, err := DownloadIndexArtifactForCommit(context.Background(), localDir, "https://github.com/acme/widgets", commitSHA)
	if err != nil {
		t.Fatalf("DownloadIndexArtifactForCommit() error: %v", err)
	}
	if !result.Downloaded {
		t.Fatalf("result.Downloaded = false, want true")
	}
	if result.Manifest.Version != 9 {
		t.Fatalf("result.Manifest.Version = %d, want 9", result.Manifest.Version)
	}
	if result.Manifest.Source != "local" {
		t.Fatalf("result.Manifest.Source = %q, want local", result.Manifest.Source)
	}
	if result.Manifest.Remote != nil {
		t.Fatalf("result.Manifest.Remote = %+v, want nil", result.Manifest.Remote)
	}

	gotDB, err := os.ReadFile(filepath.Join(localDir, "index.db"))
	if err != nil {
		t.Fatalf("os.ReadFile(index.db) error: %v", err)
	}
	if string(gotDB) != string(remoteDB) {
		t.Fatalf("downloaded db != artifact db")
	}
	gotFileHashes, err := os.ReadFile(filepath.Join(localDir, "filehashes.db"))
	if err != nil {
		t.Fatalf("os.ReadFile(filehashes.db) error: %v", err)
	}
	if string(gotFileHashes) != string(remoteFileHashes) {
		t.Fatalf("downloaded filehashes.db != artifact file hash cache")
	}

	reloaded, err := ReadManifest(localDir)
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if reloaded.Version != 9 || reloaded.IndexingHash != remoteHash || reloaded.Source != "local" || reloaded.Remote != nil {
		t.Fatalf("ReadManifest() = %+v", reloaded)
	}
}

func TestDownloadIndexArtifactForCommitReturnsNotPublishedWhenRunMissing(t *testing.T) {
	localDir := t.TempDir()
	commitSHA := "deadbeef12345678"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widgets/actions/workflows/unch-index.yml/runs":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPIBaseURL := gitHubAPIBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubAPIBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubAPIBaseURL = originalAPIBaseURL
		remoteManifestHTTPClient = originalClient
	})

	_, err := DownloadIndexArtifactForCommit(context.Background(), localDir, "https://github.com/acme/widgets", commitSHA)
	if err == nil || !errors.Is(err, ErrRemoteIndexNotPublished) {
		t.Fatalf("DownloadIndexArtifactForCommit() error = %v, want ErrRemoteIndexNotPublished", err)
	}
}

func TestDownloadIndexArtifactForCommitRejectsIncompatibleIndex(t *testing.T) {
	localDir := t.TempDir()
	commitSHA := "beadfeed12345678"
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"

	legacyDBPath := filepath.Join(t.TempDir(), "legacy-index.db")
	writeLegacyTestIndexDB(t, legacyDBPath, 11)
	legacyDB := readTestIndexDBBytes(t, legacyDBPath)
	artifactManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       11,
		IndexingHash:  "legacy-hash",
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}
	artifactZip := buildArtifactArchive(t, artifactManifest, legacyDB)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widgets/actions/workflows/unch-index.yml/runs":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{
					{
						"id":         321,
						"head_sha":   commitSHA,
						"status":     "completed",
						"conclusion": "success",
					},
				},
			})
		case "/repos/acme/widgets/actions/runs/321/artifacts":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifacts": []map[string]any{
					{
						"id":      654,
						"name":    searchIndexArtifactName,
						"expired": false,
					},
				},
			})
		case "/repos/acme/widgets/actions/artifacts/654/zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(artifactZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPIBaseURL := gitHubAPIBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubAPIBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubAPIBaseURL = originalAPIBaseURL
		remoteManifestHTTPClient = originalClient
	})

	_, err := DownloadIndexArtifactForCommit(context.Background(), localDir, "https://github.com/acme/widgets", commitSHA)
	if err == nil || !errors.Is(err, ErrRemoteIndexIncompatible) {
		t.Fatalf("DownloadIndexArtifactForCommit() error = %v, want ErrRemoteIndexIncompatible", err)
	}
	if fileExists(filepath.Join(localDir, "index.db")) {
		t.Fatalf("expected no activated local index after incompatible artifact")
	}
}

func TestDownloadIndexArtifactForCommitPaginatesWorkflowRuns(t *testing.T) {
	localDir := t.TempDir()
	commitSHA := "11223344556677889900aabbccddeeff00112233"
	ciURL := "https://github.com/acme/widgets/actions/workflows/unch-index.yml"

	remoteDBPath := filepath.Join(t.TempDir(), "artifact-index.db")
	remoteHash := writeTestIndexDB(t, remoteDBPath, 10, "internal/search/service.go", 7, "hash10", []float32{1, 0, 1})
	remoteDB := readTestIndexDBBytes(t, remoteDBPath)
	artifactManifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       10,
		IndexingHash:  remoteHash,
		Source:        "remote",
		Remote:        &Remote{CIURL: ciURL},
	}
	artifactZip := buildArtifactArchive(t, artifactManifest, remoteDB)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/widgets/actions/workflows/unch-index.yml/runs":
			page := r.URL.Query().Get("page")
			w.Header().Set("Content-Type", "application/json")
			switch page {
			case "1":
				runs := make([]map[string]any, 0, gitHubAPIPerPage)
				for i := 0; i < gitHubAPIPerPage; i++ {
					runs = append(runs, map[string]any{
						"id":         i + 1,
						"head_sha":   "ffffffffffffffffffffffffffffffffffffffff",
						"status":     "completed",
						"conclusion": "success",
					})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
			case "2":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"workflow_runs": []map[string]any{
						{
							"id":         999,
							"head_sha":   commitSHA,
							"status":     "completed",
							"conclusion": "success",
						},
					},
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]any{}})
			}
		case "/repos/acme/widgets/actions/runs/999/artifacts":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifacts": []map[string]any{
					{
						"id":      456,
						"name":    searchIndexArtifactName,
						"expired": false,
					},
				},
			})
		case "/repos/acme/widgets/actions/artifacts/456/zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(artifactZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalAPIBaseURL := gitHubAPIBaseURL
	originalClient := remoteManifestHTTPClient
	gitHubAPIBaseURL = server.URL
	remoteManifestHTTPClient = server.Client()
	t.Cleanup(func() {
		gitHubAPIBaseURL = originalAPIBaseURL
		remoteManifestHTTPClient = originalClient
	})

	result, err := DownloadIndexArtifactForCommit(context.Background(), localDir, "https://github.com/acme/widgets", commitSHA)
	if err != nil {
		t.Fatalf("DownloadIndexArtifactForCommit() error: %v", err)
	}
	if !result.Downloaded {
		t.Fatalf("result.Downloaded = false, want true")
	}
}

func TestWriteDownloadedIndexReplacesExistingDestination(t *testing.T) {
	localDir := t.TempDir()
	destPath := filepath.Join(localDir, "index.db")
	if err := os.WriteFile(destPath, []byte("old-index"), 0o644); err != nil {
		t.Fatalf("write old destination: %v", err)
	}

	remoteDBPath := filepath.Join(t.TempDir(), "replacement-index.db")
	remoteHash := writeTestIndexDB(t, remoteDBPath, 3, "internal/search/service.go", 8, "hash3", []float32{3, 3, 3})
	remoteDB := readTestIndexDBBytes(t, remoteDBPath)

	if err := writeDownloadedIndex(destPath, remoteDB, remoteHash); err != nil {
		t.Fatalf("writeDownloadedIndex() error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read replaced destination: %v", err)
	}
	if string(got) != string(remoteDB) {
		t.Fatalf("replaced destination does not match downloaded db")
	}
}

func buildArtifactArchive(t *testing.T, manifest Manifest, indexBytes []byte) []byte {
	return buildArtifactArchiveWithExtraFiles(t, manifest, indexBytes, nil)
}

func buildArtifactArchiveWithExtraFiles(t *testing.T, manifest Manifest, indexBytes []byte, extraFiles map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(manifest) error: %v", err)
	}
	manifestData = append(manifestData, '\n')

	files := map[string][]byte{
		"manifest.json": manifestData,
		"index.db":      indexBytes,
		"logs/run.log":  []byte("ok\n"),
	}
	for name, data := range extraFiles {
		files[name] = data
	}
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("writer.Create(%s) error: %v", name, err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatalf("entry.Write(%s) error: %v", name, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}
	return buf.Bytes()
}
