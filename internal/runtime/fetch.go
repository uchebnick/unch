package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const latestLlamaVersionAPI = "https://api.github.com/repos/hybridgroup/llama-cpp-builder/releases/latest"

var errYzmaArchiveNotFound = errors.New("could not download file: the requested llama.cpp version may still be building for your platform")

func fetchLatestLlamaVersion() (string, error) {
	req, err := http.NewRequest(http.MethodGet, latestLlamaVersionAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("latest llama.cpp version API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return "", fmt.Errorf("latest llama.cpp version API returned an empty tag name")
	}

	return payload.TagName, nil
}

func hasCUDA() (bool, string) {
	if runtime.GOOS == "darwin" {
		return false, ""
	}

	cmd := exec.Command("nvidia-smi")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, ""
	}

	re := regexp.MustCompile(`CUDA Version:\s*([0-9.]+)`)
	matches := re.FindStringSubmatch(out.String())
	if len(matches) >= 2 {
		return true, matches[1]
	}
	return true, ""
}

func hasROCm() (bool, string) {
	if runtime.GOOS != "linux" {
		return false, ""
	}

	cmd := exec.Command("rocminfo")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, ""
	}

	re := regexp.MustCompile(`Runtime Version:\s*([0-9.]+)`)
	matches := re.FindStringSubmatch(out.String())
	if len(matches) >= 2 {
		return true, matches[1]
	}
	return true, ""
}

func downloadModelWithContext(ctx context.Context, url, destDir string, progress ProgressTracker) error {
	_, err := downloadToTempFile(ctx, url, destDir, "model-*.gguf", progress)
	return err
}

func downloadYzmaArchive(ctx context.Context, arch, osName, processor, version, dest string, progress ProgressTracker) error {
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unknown architecture %q", arch)
	}
	if version == "" || !strings.HasPrefix(version, "b") {
		return fmt.Errorf("invalid version %q", version)
	}

	location, filename, extraURL, err := yzmaDownloadLocation(arch, osName, processor, version)
	if err != nil {
		return err
	}

	if extraURL != "" {
		if err := downloadAndExtractArchive(ctx, extraURL, dest, progress); err != nil {
			if isHTTPNotFound(err) {
				return fmt.Errorf("%w: %s", errYzmaArchiveNotFound, extraURL)
			}
			return err
		}
	}

	url := fmt.Sprintf("%s/%s", location, filename)
	if err := downloadAndExtractArchive(ctx, url, dest, progress); err != nil {
		if isHTTPNotFound(err) {
			return fmt.Errorf("%w: %s", errYzmaArchiveNotFound, url)
		}
		return err
	}
	return nil
}

func yzmaDownloadLocation(arch, osName, processor, version string) (location string, filename string, extraURL string, err error) {
	location = fmt.Sprintf("https://github.com/ggml-org/llama.cpp/releases/download/%s", version)

	switch osName {
	case "linux":
		switch processor {
		case processorCPU:
			if arch == "arm64" {
				location = fmt.Sprintf("https://github.com/hybridgroup/llama-cpp-builder/releases/download/%s", version)
				filename = fmt.Sprintf("llama-%s-bin-ubuntu-cpu-arm64.tar.gz", version)
				return location, filename, "", nil
			}
			filename = fmt.Sprintf("llama-%s-bin-ubuntu-x64.tar.gz", version)
			return location, filename, "", nil
		case processorCUDA:
			location = fmt.Sprintf("https://github.com/hybridgroup/llama-cpp-builder/releases/download/%s", version)
			if arch == "arm64" {
				filename = fmt.Sprintf("llama-%s-bin-ubuntu-cuda-arm64.tar.gz", version)
			} else {
				filename = fmt.Sprintf("llama-%s-bin-ubuntu-cuda-13-x64.tar.gz", version)
			}
			return location, filename, "", nil
		case processorVulkan:
			if arch == "arm64" {
				location = fmt.Sprintf("https://github.com/hybridgroup/llama-cpp-builder/releases/download/%s", version)
				filename = fmt.Sprintf("llama-%s-bin-ubuntu-vulkan-arm64.tar.gz", version)
			} else {
				filename = fmt.Sprintf("llama-%s-bin-ubuntu-vulkan-x64.tar.gz", version)
			}
			return location, filename, "", nil
		case processorROCm:
			if arch != "amd64" {
				return "", "", "", fmt.Errorf("precompiled binaries for Linux ARM64 ROCm are not available")
			}
			filename = fmt.Sprintf("llama-%s-bin-ubuntu-rocm-7.2-x64.tar.gz", version)
			return location, filename, "", nil
		default:
			return "", "", "", fmt.Errorf("unknown processor %q", processor)
		}
	case "darwin":
		switch processor {
		case processorMetal:
			if arch != "arm64" {
				return "", "", "", fmt.Errorf("precompiled binaries for macOS non-ARM64 Metal are not available")
			}
			filename = fmt.Sprintf("llama-%s-bin-macos-arm64.tar.gz", version)
			return location, filename, "", nil
		case processorCPU:
			if arch == "arm64" {
				filename = fmt.Sprintf("llama-%s-bin-macos-arm64.tar.gz", version)
			} else {
				filename = fmt.Sprintf("llama-%s-bin-macos-x64.tar.gz", version)
			}
			return location, filename, "", nil
		default:
			return "", "", "", fmt.Errorf("unknown processor %q", processor)
		}
	case "windows":
		switch processor {
		case processorCPU:
			if arch == "arm64" {
				filename = fmt.Sprintf("llama-%s-bin-win-cpu-arm64.zip", version)
			} else {
				filename = fmt.Sprintf("llama-%s-bin-win-cpu-x64.zip", version)
			}
			return location, filename, "", nil
		case processorCUDA:
			if arch == "arm64" {
				return "", "", "", fmt.Errorf("precompiled binaries for Windows ARM64 CUDA are not available")
			}
			filename = fmt.Sprintf("llama-%s-bin-win-cuda-13.1-x64.zip", version)
			extraURL = fmt.Sprintf("%s/%s", location, "cudart-llama-bin-win-cuda-13.1-x64.zip")
			return location, filename, extraURL, nil
		case processorVulkan:
			if arch == "arm64" {
				return "", "", "", fmt.Errorf("precompiled binaries for Windows ARM64 Vulkan are not available")
			}
			filename = fmt.Sprintf("llama-%s-bin-win-vulkan-x64.zip", version)
			return location, filename, "", nil
		case processorROCm:
			if arch != "amd64" {
				return "", "", "", fmt.Errorf("precompiled binaries for Windows ARM64 ROCm are not available")
			}
			filename = fmt.Sprintf("llama-%s-bin-win-hip-radeon-x64.zip", version)
			return location, filename, "", nil
		default:
			return "", "", "", fmt.Errorf("unknown processor %q", processor)
		}
	default:
		return "", "", "", fmt.Errorf("unknown operating system %q", osName)
	}
}

func downloadAndExtractArchive(ctx context.Context, url, destDir string, progress ProgressTracker) error {
	pattern, archiveType, err := archiveDownloadPattern(url)
	if err != nil {
		return err
	}

	archivePath, err := downloadToTempFile(ctx, url, destDir, pattern, progress)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(archivePath)
	}()

	switch archiveType {
	case ".tar.gz":
		return extractTarGz(archivePath, destDir)
	case ".zip":
		return extractZIP(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive type for %s", url)
	}
}

func archiveDownloadPattern(url string) (string, string, error) {
	switch {
	case strings.HasSuffix(strings.ToLower(url), ".tar.gz"):
		return "archive-*.tar.gz", ".tar.gz", nil
	case strings.HasSuffix(strings.ToLower(url), ".zip"):
		return "archive-*.zip", ".zip", nil
	default:
		return "", "", fmt.Errorf("unsupported archive type for %s", url)
	}
}

func downloadToTempFile(ctx context.Context, url, destDir, pattern string, progress ProgressTracker) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create download dir: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		return "", fmt.Errorf("http %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	tmpFile, err := os.CreateTemp(destDir, pattern)
	if err != nil {
		_ = resp.Body.Close()
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	stream := io.ReadCloser(resp.Body)
	if progress != nil {
		stream = progress.TrackProgress(resp.Request.URL.String(), 0, resp.ContentLength, resp.Body)
	}

	_, copyErr := io.Copy(tmpFile, stream)
	closeErr := tmpFile.Close()
	streamCloseErr := stream.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("copy response body: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", closeErr)
	}
	if streamCloseErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close response body: %w", streamCloseErr)
	}

	return tmpPath, nil
}

func extractTarGz(archivePath, destDir string) error {
	root, err := prepareArchiveRoot(destDir)
	if err != nil {
		return err
	}

	var pendingLinks []archiveLink

	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() {
		_ = gzr.Close()
	}()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return materializeArchiveLinks(root, pendingLinks)
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		if archivePathHasParentRef(header.Name) {
			return fmt.Errorf("archive entry %q escapes destination", header.Name)
		}

		name, err := archiveEntryName(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := ensureArchiveDir(root, name, dirMode(os.FileMode(header.Mode))); err != nil {
				return fmt.Errorf("create directory %s: %w", name, err)
			}
		case tar.TypeReg:
			target, err := archiveTargetPath(root, name)
			if err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", target, err)
			}
		case tar.TypeSymlink:
			target, err := archiveTargetPath(root, name)
			if err != nil {
				return err
			}
			pendingLinks = append(pendingLinks, archiveLink{
				linkPath:   target,
				targetPath: header.Linkname,
			})
		case tar.TypeLink:
			target, err := archiveTargetPath(root, name)
			if err != nil {
				return err
			}
			pendingLinks = append(pendingLinks, archiveLink{
				linkPath:   target,
				targetPath: header.Linkname,
			})
		}
	}
}

func extractZIP(archivePath, destDir string) error {
	root, err := prepareArchiveRoot(destDir)
	if err != nil {
		return err
	}

	var pendingLinks []archiveLink

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	for _, file := range reader.File {
		if archivePathHasParentRef(file.Name) {
			return fmt.Errorf("archive entry %q escapes destination", file.Name)
		}

		name, err := archiveEntryName(file.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		mode := file.Mode()
		if file.FileInfo().IsDir() {
			if _, err := ensureArchiveDir(root, name, dirMode(mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", name, err)
			}
			continue
		}

		target, err := archiveTargetPath(root, name)
		if err != nil {
			return err
		}

		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}

		if mode&os.ModeSymlink != 0 {
			linkTarget, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return fmt.Errorf("read symlink target from %s: %w", file.Name, err)
			}
			pendingLinks = append(pendingLinks, archiveLink{
				linkPath:   target,
				targetPath: string(linkTarget),
			})
			continue
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode(mode))
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("create file %s: %w", target, err)
		}

		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return fmt.Errorf("write file %s: %w", target, err)
		}
		if err := out.Close(); err != nil {
			_ = rc.Close()
			return fmt.Errorf("close file %s: %w", target, err)
		}
		if err := rc.Close(); err != nil {
			return fmt.Errorf("close zip entry %s: %w", file.Name, err)
		}
	}

	return materializeArchiveLinks(root, pendingLinks)
}

func prepareArchiveRoot(destDir string) (string, error) {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve destination root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create destination root %s: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve destination root symlinks: %w", err)
	}
	return resolved, nil
}

func archiveEntryName(raw string) (string, error) {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return "", nil
	}
	if idx := strings.IndexByte(name, '/'); idx >= 0 {
		name = strings.TrimPrefix(name[idx+1:], "/")
	}
	if name == "" {
		return "", nil
	}

	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == "" {
		return "", nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", raw)
	}
	return clean, nil
}

func archiveTargetPath(root, name string) (string, error) {
	parent, err := ensureArchiveDir(root, filepath.Dir(name), 0o755)
	if err != nil {
		return "", err
	}

	target := filepath.Join(parent, filepath.Base(name))
	if !pathWithinRoot(root, target) {
		return "", fmt.Errorf("archive entry %q escapes destination", name)
	}

	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("archive entry %q would overwrite existing symlink", name)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat extracted path %s: %w", target, err)
	}

	return target, nil
}

func ensureArchiveDir(root, relDir string, mode os.FileMode) (string, error) {
	current := root
	relDir = filepath.Clean(relDir)
	if relDir == "." || relDir == "" {
		return current, nil
	}

	for _, component := range strings.Split(relDir, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return "", fmt.Errorf("archive path %q escapes destination", relDir)
		}

		next := filepath.Join(current, component)
		info, err := os.Lstat(next)
		switch {
		case os.IsNotExist(err):
			if err := os.Mkdir(next, dirMode(mode)); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", next, err)
			}
			current = next
		case err != nil:
			return "", fmt.Errorf("stat %s: %w", next, err)
		case info.Mode()&os.ModeSymlink != 0:
			resolved, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", fmt.Errorf("resolve symlink %s: %w", next, err)
			}
			if !pathWithinRoot(root, resolved) {
				return "", fmt.Errorf("archive path %q escapes destination through symlink", relDir)
			}
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				return "", fmt.Errorf("stat resolved symlink %s: %w", resolved, err)
			}
			if !resolvedInfo.IsDir() {
				return "", fmt.Errorf("archive path %q traverses non-directory symlink", relDir)
			}
			current = resolved
		case info.IsDir():
			current = next
		default:
			return "", fmt.Errorf("archive path %q traverses non-directory entry %s", relDir, next)
		}
	}

	return current, nil
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func dirMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o755
	}
	return mode | 0o755
}

func fileMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o644
	}
	return mode
}

func isHTTPNotFound(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "http 404")
}

type archiveLink struct {
	linkPath   string
	targetPath string
}

func archivePathHasParentRef(name string) bool {
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.Contains(name, "..")
}

func materializeArchiveLinks(root string, links []archiveLink) error {
	for _, link := range links {
		if err := materializeArchiveLink(root, link); err != nil {
			return err
		}
	}
	return nil
}

func materializeArchiveLink(root string, link archiveLink) error {
	parentDir, err := filepath.EvalSymlinks(filepath.Dir(link.linkPath))
	if err != nil {
		return fmt.Errorf("resolve archive link parent: %w", err)
	}
	if !pathWithinRoot(root, parentDir) {
		return fmt.Errorf("archive link parent escapes destination")
	}
	if _, err := os.Lstat(link.linkPath); err == nil {
		return fmt.Errorf("archive link destination %s already exists", link.linkPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat archive link destination %s: %w", link.linkPath, err)
	}

	resolvedTarget, err := resolvedArchiveLinkTarget(parentDir, root, link.targetPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return fmt.Errorf("stat archive link target %s: %w", resolvedTarget, err)
	}
	if info.IsDir() {
		return fmt.Errorf("archive link target %s is a directory", resolvedTarget)
	}

	if err := copyFile(resolvedTarget, link.linkPath, fileMode(info.Mode())); err != nil {
		return fmt.Errorf("materialize archive link %s from %s: %w", link.linkPath, resolvedTarget, err)
	}
	return nil
}

func resolvedArchiveLinkTarget(parentDir, root, rawTarget string) (string, error) {
	linkTarget := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rawTarget)))
	if linkTarget == "." || linkTarget == "" || filepath.IsAbs(linkTarget) {
		return "", fmt.Errorf("invalid archive link target %q", rawTarget)
	}

	candidateTarget := filepath.Clean(filepath.Join(parentDir, linkTarget))
	if !pathWithinRoot(root, candidateTarget) {
		return "", fmt.Errorf("archive link target %q escapes destination", rawTarget)
	}

	resolvedTarget, err := filepath.EvalSymlinks(candidateTarget)
	if err != nil {
		return "", fmt.Errorf("resolve archive link target %q: %w", rawTarget, err)
	}
	if !pathWithinRoot(root, resolvedTarget) {
		return "", fmt.Errorf("archive link target %q escapes destination", rawTarget)
	}
	return resolvedTarget, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
