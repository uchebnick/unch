package semsearch

import (
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

	"github.com/uchebnick/unch-searcher/internal/indexdb"
)

const (
	gitHubActionsWorkflowSegment = "actions/workflows"
	defaultGitHubContentBaseURL  = "https://raw.githubusercontent.com"
	gitHubPagesBranch            = "gh-pages"
	gitHubPagesSemsearchDir      = "semsearch"
	defaultCIWorkflowFile        = "searcher.yml"
)

var (
	ErrRemoteIndexNotPublished = errors.New("remote index is not published yet; run the searcher GitHub Actions workflow once to publish it")
	gitHubContentBaseURL       = defaultGitHubContentBaseURL
	remoteManifestHTTPClient   = &http.Client{Timeout: 20 * time.Second}
)

type GitHubWorkflowRef struct {
	Owner        string
	Repo         string
	WorkflowFile string
}

type RemoteSyncResult struct {
	Checked    bool
	Downloaded bool
	Manifest   Manifest
	Note       string
}

func HasRemoteBinding(manifest Manifest) bool {
	return manifest.Source == "remote" && manifest.Remote != nil && strings.TrimSpace(manifest.Remote.CIURL) != ""
}

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

func (w GitHubWorkflowRef) CIURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/%s/%s", w.Owner, w.Repo, gitHubActionsWorkflowSegment, w.WorkflowFile)
}

func (w GitHubWorkflowRef) PublishedManifestURL() (string, error) {
	return joinURLPath(gitHubContentBaseURL, w.Owner, w.Repo, gitHubPagesBranch, gitHubPagesSemsearchDir, "manifest.json")
}

func (w GitHubWorkflowRef) PublishedIndexURL() (string, error) {
	return joinURLPath(gitHubContentBaseURL, w.Owner, w.Repo, gitHubPagesBranch, gitHubPagesSemsearchDir, "index.db")
}

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

	if strings.TrimSpace(expectedHash) != "" {
		gotHash, err := indexdb.LogicalHash(ctx, tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
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

func fetchRemoteBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET %s: %w", rawURL, err)
	}

	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := remoteManifestHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
