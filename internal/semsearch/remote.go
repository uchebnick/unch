package semsearch

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/uchebnick/unch/internal/indexdb"
)

const (
	gitHubActionsWorkflowSegment = "actions/workflows"
	defaultGitHubAPIBaseURL      = "https://api.github.com"
	defaultGitHubContentBaseURL  = "https://raw.githubusercontent.com"
	gitHubPagesBranch            = "gh-pages"
	gitHubPagesSemsearchDir      = "semsearch"
	defaultCIWorkflowFile        = "searcher.yml"
	searchIndexArtifactName      = "semsearch-index"
)

var (
	ErrRemoteIndexNotPublished = errors.New("remote index is not published yet; run the searcher GitHub Actions workflow once to publish it")
	ErrRemoteIndexIncompatible = errors.New("remote index uses an incompatible schema; rerun the searcher GitHub Actions workflow to publish a compatible index")
	gitHubAPIBaseURL           = defaultGitHubAPIBaseURL
	gitHubContentBaseURL       = defaultGitHubContentBaseURL
	remoteManifestHTTPClient   = &http.Client{Timeout: 20 * time.Second}
)

// GitHubWorkflowRef identifies one workflow file inside a GitHub repository.
type GitHubWorkflowRef struct {
	Owner        string
	Repo         string
	WorkflowFile string
}

// RemoteSyncResult summarizes whether a remote check ran and whether it updated the local cache.
type RemoteSyncResult struct {
	Checked    bool
	Downloaded bool
	Manifest   Manifest
	Note       string
}

// ArtifactDownloadResult describes a one-shot artifact download for a specific commit.
type ArtifactDownloadResult struct {
	Downloaded bool
	CommitSHA  string
	Manifest   Manifest
	Note       string
}

// HasRemoteBinding reports whether the manifest is currently bound to a remote CI workflow.
func HasRemoteBinding(manifest Manifest) bool {
	return manifest.Source == "remote" && manifest.Remote != nil && strings.TrimSpace(manifest.Remote.CIURL) != ""
}

// BindRemoteManifest switches the local manifest into remote mode for the provided CI workflow URL.
func BindRemoteManifest(localDir string, ciURL string) (Manifest, error) {
	resolvedCIURL, err := ResolveGitHubCIURL(ciURL)
	if err != nil {
		return Manifest{}, err
	}

	manifest, _, err := EnsureManifest(localDir)
	if err != nil {
		return Manifest{}, err
	}

	manifest.Source = "remote"
	manifest.Remote = &Remote{CIURL: resolvedCIURL}

	if err := WriteManifest(localDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// DetectGitHubCIURL derives the default workflow URL from git remote origin.
func DetectGitHubCIURL(root string) (string, error) {
	originURL, err := gitOriginURL(root)
	if err != nil {
		return "", err
	}

	owner, repo, err := parseGitHubRepositoryURL(originURL)
	if err != nil {
		return "", err
	}

	return GitHubWorkflowRef{
		Owner:        owner,
		Repo:         repo,
		WorkflowFile: defaultCIWorkflowFile,
	}.CIURL(), nil
}

// ResolveGitHubCIURL accepts either a repository URL or a full workflow URL and normalizes it.
func ResolveGitHubCIURL(input string) (string, error) {
	if workflow, err := ParseGitHubWorkflowURL(input); err == nil {
		return workflow.CIURL(), nil
	}

	owner, repo, err := parseGitHubRepositoryURL(input)
	if err == nil {
		return GitHubWorkflowRef{
			Owner:        owner,
			Repo:         repo,
			WorkflowFile: defaultCIWorkflowFile,
		}.CIURL(), nil
	}

	return "", fmt.Errorf(
		"unsupported GitHub CI target %q; pass a repository URL like https://github.com/owner/repo or a workflow URL like https://github.com/owner/repo/actions/workflows/%s",
		strings.TrimSpace(input),
		defaultCIWorkflowFile,
	)
}

// ParseGitHubWorkflowURL validates and parses a GitHub Actions workflow URL.
func ParseGitHubWorkflowURL(ciURL string) (GitHubWorkflowRef, error) {
	ciURL = strings.TrimSpace(ciURL)
	if ciURL == "" {
		return GitHubWorkflowRef{}, fmt.Errorf("empty GitHub workflow URL")
	}

	parsed, err := url.Parse(ciURL)
	if err != nil {
		return GitHubWorkflowRef{}, fmt.Errorf("parse GitHub workflow URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return GitHubWorkflowRef{}, fmt.Errorf("unsupported workflow URL scheme %q", parsed.Scheme)
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return GitHubWorkflowRef{}, fmt.Errorf("workflow URL host must be github.com, got %q", parsed.Hostname())
	}

	parts := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
	if len(parts) != 5 || parts[2] != "actions" || parts[3] != "workflows" {
		return GitHubWorkflowRef{}, fmt.Errorf("unsupported GitHub workflow URL path %q", parsed.Path)
	}

	workflowFile := strings.TrimSpace(parts[4])
	if workflowFile == "" {
		return GitHubWorkflowRef{}, fmt.Errorf("workflow URL does not include a workflow file")
	}

	return GitHubWorkflowRef{
		Owner:        parts[0],
		Repo:         strings.TrimSuffix(parts[1], ".git"),
		WorkflowFile: workflowFile,
	}, nil
}

// CIURL reconstructs the canonical GitHub workflow URL.
func (w GitHubWorkflowRef) CIURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/%s/%s", w.Owner, w.Repo, gitHubActionsWorkflowSegment, w.WorkflowFile)
}

// PublishedManifestURL returns the raw gh-pages manifest URL for this workflow.
func (w GitHubWorkflowRef) PublishedManifestURL() (string, error) {
	return joinURLPath(gitHubContentBaseURL, w.Owner, w.Repo, gitHubPagesBranch, gitHubPagesSemsearchDir, "manifest.json")
}

// PublishedIndexURL returns the raw gh-pages index database URL for this workflow.
func (w GitHubWorkflowRef) PublishedIndexURL() (string, error) {
	return joinURLPath(gitHubContentBaseURL, w.Owner, w.Repo, gitHubPagesBranch, gitHubPagesSemsearchDir, "index.db")
}

// SyncRemoteIndex refreshes the local index from the published remote CI artifacts when needed.
func SyncRemoteIndex(ctx context.Context, localDir string) (RemoteSyncResult, error) {
	manifest, err := ReadManifest(localDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RemoteSyncResult{}, nil
		}
		return RemoteSyncResult{}, err
	}

	if !HasRemoteBinding(manifest) {
		return RemoteSyncResult{Manifest: manifest}, nil
	}

	workflow, err := ParseGitHubWorkflowURL(manifest.Remote.CIURL)
	if err != nil {
		return RemoteSyncResult{}, fmt.Errorf("parse remote workflow URL: %w", err)
	}

	remoteManifest, err := fetchPublishedManifest(ctx, workflow)
	if err != nil {
		return handleRemoteManifestFetchError(err, localDir, manifest)
	}

	remoteManifest = normalizePublishedManifest(remoteManifest, manifest.Remote.CIURL)

	dbPath := filepath.Join(localDir, "index.db")
	dbExists := fileExists(dbPath)
	needsDownload := !dbExists ||
		manifest.Version != remoteManifest.Version ||
		manifest.IndexingHash != remoteManifest.IndexingHash

	if !needsDownload {
		if !manifestsEqual(manifest, remoteManifest) {
			if err := WriteManifest(localDir, remoteManifest); err != nil {
				return RemoteSyncResult{}, fmt.Errorf("write refreshed manifest: %w", err)
			}
		}
		return RemoteSyncResult{
			Checked:  true,
			Manifest: remoteManifest,
			Note:     fmt.Sprintf("Remote index is up to date at version %d", remoteManifest.Version),
		}, nil
	}

	if err := downloadPublishedIndex(ctx, workflow, dbPath, remoteManifest.IndexingHash); err != nil {
		if errors.Is(err, ErrRemoteIndexIncompatible) {
			if dbExists {
				return RemoteSyncResult{
					Checked:  true,
					Manifest: manifest,
					Note:     "Published remote index uses an older schema; using the local cache until the searcher workflow republishes it",
				}, nil
			}
			if err := WriteManifest(localDir, remoteManifest); err != nil {
				return RemoteSyncResult{}, fmt.Errorf("write refreshed manifest after incompatible remote index: %w", err)
			}
		}
		return RemoteSyncResult{}, fmt.Errorf("download remote index: %w", err)
	}
	if err := WriteManifest(localDir, remoteManifest); err != nil {
		return RemoteSyncResult{}, fmt.Errorf("write downloaded manifest: %w", err)
	}

	return RemoteSyncResult{
		Checked:    true,
		Downloaded: true,
		Manifest:   remoteManifest,
		Note:       fmt.Sprintf("Downloaded remote index version %d", remoteManifest.Version),
	}, nil
}

// DownloadIndexArtifactForCommit downloads the search index artifact for one commit without binding the local manifest to a remote workflow.
func DownloadIndexArtifactForCommit(ctx context.Context, localDir string, ciTarget string, commitSHA string) (ArtifactDownloadResult, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return ArtifactDownloadResult{}, fmt.Errorf("empty commit SHA")
	}

	resolvedCIURL, err := ResolveGitHubCIURL(ciTarget)
	if err != nil {
		return ArtifactDownloadResult{}, err
	}
	workflow, err := ParseGitHubWorkflowURL(resolvedCIURL)
	if err != nil {
		return ArtifactDownloadResult{}, fmt.Errorf("parse remote workflow URL: %w", err)
	}

	runID, artifactID, err := findSearchArtifactForCommit(ctx, workflow, commitSHA)
	if err != nil {
		return ArtifactDownloadResult{}, err
	}

	artifactZip, err := downloadArtifactArchive(ctx, workflow, artifactID)
	if err != nil {
		return ArtifactDownloadResult{}, err
	}

	downloadedManifest, indexBytes, err := extractIndexArtifactPayload(artifactZip)
	if err != nil {
		return ArtifactDownloadResult{}, err
	}

	localManifest := normalizeDownloadedArtifactManifest(downloadedManifest)
	dbPath := filepath.Join(localDir, "index.db")
	if err := writeDownloadedIndex(dbPath, indexBytes, downloadedManifest.IndexingHash); err != nil {
		return ArtifactDownloadResult{}, fmt.Errorf("activate artifact index: %w", err)
	}
	if err := WriteManifest(localDir, localManifest); err != nil {
		return ArtifactDownloadResult{}, fmt.Errorf("write downloaded manifest: %w", err)
	}

	return ArtifactDownloadResult{
		Downloaded: true,
		CommitSHA:  commitSHA,
		Manifest:   localManifest,
		Note:       fmt.Sprintf("Downloaded search index artifact for %s from workflow run %d", shortenCommitSHA(commitSHA), runID),
	}, nil
}

func normalizePublishedManifest(remoteManifest Manifest, ciURL string) Manifest {
	remoteManifest = remoteManifest.Normalize()
	if remoteManifest.Source == "" {
		remoteManifest.Source = "remote"
	}
	remoteManifest.Source = "remote"
	if remoteManifest.Remote == nil || strings.TrimSpace(remoteManifest.Remote.CIURL) == "" {
		remoteManifest.Remote = &Remote{CIURL: strings.TrimSpace(ciURL)}
	}
	return remoteManifest
}

func normalizeDownloadedArtifactManifest(manifest Manifest) Manifest {
	manifest = manifest.Normalize()
	manifest.Source = "local"
	manifest.Remote = nil
	return manifest
}

func fetchPublishedManifest(ctx context.Context, workflow GitHubWorkflowRef) (Manifest, error) {
	manifestURL, err := workflow.PublishedManifestURL()
	if err != nil {
		return Manifest{}, err
	}

	data, err := fetchRemoteBytes(ctx, manifestURL)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode published manifest: %w", err)
	}
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate published manifest: %w", err)
	}
	return manifest, nil
}

func downloadPublishedIndex(ctx context.Context, workflow GitHubWorkflowRef, destPath string, expectedHash string) error {
	indexURL, err := workflow.PublishedIndexURL()
	if err != nil {
		return err
	}

	data, err := fetchRemoteBytes(ctx, indexURL)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}

	tmpPath := destPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}

	return activateDownloadedIndex(ctx, tmpPath, destPath, expectedHash)
}

func writeDownloadedIndex(destPath string, data []byte, expectedHash string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}

	tmpPath := destPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}

	return activateDownloadedIndex(context.Background(), tmpPath, destPath, expectedHash)
}

func activateDownloadedIndex(ctx context.Context, tmpPath string, destPath string, expectedHash string) error {
	if strings.TrimSpace(expectedHash) != "" {
		gotHash, err := indexdb.LogicalHash(ctx, tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			if errors.Is(err, indexdb.ErrIncompatibleSchema) {
				return fmt.Errorf("%w: %v", ErrRemoteIndexIncompatible, err)
			}
			return fmt.Errorf("hash %s: %w", tmpPath, err)
		}
		if gotHash != expectedHash {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("downloaded index hash mismatch: got %s want %s", gotHash, expectedHash)
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("activate %s: %w", destPath, err)
	}
	return nil
}

type gitHubWorkflowRunsResponse struct {
	WorkflowRuns []gitHubWorkflowRun `json:"workflow_runs"`
}

type gitHubWorkflowRun struct {
	ID         int64  `json:"id"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type gitHubArtifactsResponse struct {
	Artifacts []gitHubArtifact `json:"artifacts"`
}

type gitHubArtifact struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Expired bool   `json:"expired"`
}

func findSearchArtifactForCommit(ctx context.Context, workflow GitHubWorkflowRef, commitSHA string) (int64, int64, error) {
	runs, err := listWorkflowRuns(ctx, workflow, commitSHA)
	if err != nil {
		return 0, 0, err
	}

	for _, run := range runs {
		if !strings.EqualFold(strings.TrimSpace(run.HeadSHA), commitSHA) {
			continue
		}
		if strings.TrimSpace(run.Conclusion) != "success" {
			continue
		}

		artifactID, ok, err := findRunArtifact(ctx, workflow, run.ID)
		if err != nil {
			return 0, 0, err
		}
		if ok {
			return run.ID, artifactID, nil
		}
	}

	return 0, 0, fmt.Errorf("%w for commit %s", ErrRemoteIndexNotPublished, shortenCommitSHA(commitSHA))
}

func listWorkflowRuns(ctx context.Context, workflow GitHubWorkflowRef, commitSHA string) ([]gitHubWorkflowRun, error) {
	runsURL, err := workflowRunsAPIURL(workflow, commitSHA)
	if err != nil {
		return nil, err
	}

	data, err := fetchRemoteBytes(ctx, runsURL)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w for commit %s", ErrRemoteIndexNotPublished, shortenCommitSHA(commitSHA))
		}
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}

	var payload gitHubWorkflowRunsResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode workflow runs: %w", err)
	}
	return payload.WorkflowRuns, nil
}

func findRunArtifact(ctx context.Context, workflow GitHubWorkflowRef, runID int64) (int64, bool, error) {
	artifactsURL, err := runArtifactsAPIURL(workflow, runID)
	if err != nil {
		return 0, false, err
	}

	data, err := fetchRemoteBytes(ctx, artifactsURL)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("list workflow artifacts: %w", err)
	}

	var payload gitHubArtifactsResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false, fmt.Errorf("decode workflow artifacts: %w", err)
	}

	for _, artifact := range payload.Artifacts {
		if artifact.Name == searchIndexArtifactName && !artifact.Expired {
			return artifact.ID, true, nil
		}
	}
	return 0, false, nil
}

func downloadArtifactArchive(ctx context.Context, workflow GitHubWorkflowRef, artifactID int64) ([]byte, error) {
	artifactURL, err := artifactArchiveAPIURL(workflow, artifactID)
	if err != nil {
		return nil, err
	}

	data, err := fetchRemoteBytes(ctx, artifactURL)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: artifact %d is unavailable", ErrRemoteIndexNotPublished, artifactID)
		}
		return nil, fmt.Errorf("download artifact archive: %w", err)
	}
	return data, nil
}

func extractIndexArtifactPayload(archive []byte) (Manifest, []byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("open artifact archive: %w", err)
	}

	var manifestBytes []byte
	var indexBytes []byte
	for _, file := range reader.File {
		switch path.Base(file.Name) {
		case "manifest.json":
			manifestBytes, err = readZipFile(file)
		case "index.db":
			indexBytes, err = readZipFile(file)
		default:
			continue
		}
		if err != nil {
			return Manifest{}, nil, err
		}
	}

	if len(manifestBytes) == 0 {
		return Manifest{}, nil, fmt.Errorf("artifact archive does not include manifest.json")
	}
	if len(indexBytes) == 0 {
		return Manifest{}, nil, fmt.Errorf("artifact archive does not include index.db")
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Manifest{}, nil, fmt.Errorf("decode artifact manifest: %w", err)
	}
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, fmt.Errorf("validate artifact manifest: %w", err)
	}
	return manifest, indexBytes, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open artifact member %s: %w", file.Name, err)
	}
	defer func() {
		_ = rc.Close()
	}()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read artifact member %s: %w", file.Name, err)
	}
	return data, nil
}

func fetchRemoteBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET %s: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := remoteManifestHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: unexpected status %s", rawURL, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return data, nil
}

func handleRemoteManifestFetchError(fetchErr error, localDir string, manifest Manifest) (RemoteSyncResult, error) {
	dbPath := filepath.Join(localDir, "index.db")
	dbExists := fileExists(dbPath)

	if errors.Is(fetchErr, os.ErrNotExist) {
		if dbExists {
			return RemoteSyncResult{
				Checked:  true,
				Manifest: manifest,
				Note:     "Remote index is not published yet; using the local cache until the searcher workflow publishes it",
			}, nil
		}
		return RemoteSyncResult{}, ErrRemoteIndexNotPublished
	}

	if dbExists {
		return RemoteSyncResult{
			Checked:  true,
			Manifest: manifest,
			Note:     fmt.Sprintf("Warning: could not refresh remote index (%v); using local cache", fetchErr),
		}, nil
	}

	return RemoteSyncResult{}, fmt.Errorf("refresh remote manifest: %w", fetchErr)
}

func manifestsEqual(a Manifest, b Manifest) bool {
	a = a.Normalize()
	b = b.Normalize()

	if a.SchemaVersion != b.SchemaVersion ||
		a.Version != b.Version ||
		a.IndexingHash != b.IndexingHash ||
		a.Source != b.Source {
		return false
	}

	switch {
	case a.Remote == nil && b.Remote == nil:
		return true
	case a.Remote == nil || b.Remote == nil:
		return false
	default:
		return a.Remote.CIURL == b.Remote.CIURL
	}
}

func gitOriginURL(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect git origin URL: %w", err)
	}

	originURL := strings.TrimSpace(string(output))
	if originURL == "" {
		return "", fmt.Errorf("detect git origin URL: empty remote.origin.url")
	}
	return originURL, nil
}

func parseGitHubRepositoryURL(originURL string) (string, string, error) {
	originURL = strings.TrimSpace(originURL)
	if originURL == "" {
		return "", "", fmt.Errorf("empty GitHub repository URL")
	}

	if strings.HasPrefix(originURL, "git@github.com:") {
		trimmed := strings.TrimPrefix(originURL, "git@github.com:")
		parts := strings.Split(strings.Trim(strings.TrimSuffix(trimmed, ".git"), "/"), "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("unsupported GitHub SSH URL %q", originURL)
		}
		return parts[0], parts[1], nil
	}

	parsed, err := url.Parse(originURL)
	if err != nil {
		return "", "", fmt.Errorf("parse GitHub repository URL: %w", err)
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", fmt.Errorf("repository host must be github.com, got %q", parsed.Hostname())
	}

	parts := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unsupported GitHub repository path %q", parsed.Path)
	}

	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func joinURLPath(base string, elems ...string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", base, err)
	}
	parts := append([]string{parsed.Path}, elems...)
	parsed.Path = path.Join(parts...)
	return parsed.String(), nil
}

func workflowRunsAPIURL(workflow GitHubWorkflowRef, commitSHA string) (string, error) {
	base, err := joinURLPath(gitHubAPIBaseURL, "repos", workflow.Owner, workflow.Repo, "actions", "workflows", workflow.WorkflowFile, "runs")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse workflow runs URL %q: %w", base, err)
	}
	query := parsed.Query()
	query.Set("head_sha", strings.TrimSpace(commitSHA))
	query.Set("per_page", "100")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func runArtifactsAPIURL(workflow GitHubWorkflowRef, runID int64) (string, error) {
	return joinURLPath(gitHubAPIBaseURL, "repos", workflow.Owner, workflow.Repo, "actions", "runs", fmt.Sprintf("%d", runID), "artifacts")
}

func artifactArchiveAPIURL(workflow GitHubWorkflowRef, artifactID int64) (string, error) {
	return joinURLPath(gitHubAPIBaseURL, "repos", workflow.Owner, workflow.Repo, "actions", "artifacts", fmt.Sprintf("%d", artifactID), "zip")
}

func shortenCommitSHA(commitSHA string) string {
	commitSHA = strings.TrimSpace(commitSHA)
	if len(commitSHA) <= 12 {
		return commitSHA
	}
	return commitSHA[:12]
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
