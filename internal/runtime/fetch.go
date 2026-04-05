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
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
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
	filename, err := downloadFileToDir(ctx, url, destDir, progress)
	if err != nil {
		return err
	}
	if strings.TrimSpace(filename) == "" {
		return fmt.Errorf("downloaded model from %s but could not determine filename", url)
	}
	return nil
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
	archivePath, err := downloadFile(ctx, url, destDir, progress)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(archivePath)
	}()

	switch {
	case strings.HasSuffix(strings.ToLower(url), ".tar.gz"):
		return extractTarGz(archivePath, destDir)
	case strings.HasSuffix(strings.ToLower(url), ".zip"):
		return extractZIP(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive type for %s", url)
	}
}

func downloadFileToDir(ctx context.Context, url, destDir string, progress ProgressTracker) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create destination dir: %w", err)
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("http %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	filename := downloadFilename(resp)
	if filename == "" {
		filename = "download.bin"
	}

	dst := filepath.Join(destDir, filepath.Base(filename))
	if _, err := downloadResponseBody(resp, dst, progress); err != nil {
		return "", err
	}

	return filepath.Base(dst), nil
}

func downloadFile(ctx context.Context, url, destDir string, progress ProgressTracker) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create destination dir: %w", err)
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("http %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	filename := downloadFilename(resp)
	if filename == "" {
		filename = path.Base(resp.Request.URL.Path)
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "download.bin"
	}

	dst := filepath.Join(destDir, filepath.Base(filename))
	if _, err := downloadResponseBody(resp, dst, progress); err != nil {
		return "", err
	}

	return dst, nil
}

func downloadResponseBody(resp *http.Response, dst string, progress ProgressTracker) (int64, error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("close temp file: %w", err)
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("open temp file: %w", err)
	}

	stream := resp.Body
	if progress != nil {
		stream = progress.TrackProgress(resp.Request.URL.String(), 0, resp.ContentLength, resp.Body)
	}

	written, copyErr := io.Copy(out, stream)
	closeErr := out.Close()
	streamCloseErr := stream.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("copy response body: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("close temp file: %w", closeErr)
	}
	if streamCloseErr != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("close response body: %w", streamCloseErr)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return 0, fmt.Errorf("move downloaded file into place: %w", err)
	}

	return written, nil
}

func downloadFilename(resp *http.Response) string {
	if cd := strings.TrimSpace(resp.Header.Get("Content-Disposition")); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := strings.TrimSpace(params["filename"]); filename != "" {
				return filepath.Base(filename)
			}
		}
	}

	if resp.Request != nil && resp.Request.URL != nil {
		if base := path.Base(resp.Request.URL.Path); base != "" && base != "." && base != "/" {
			return filepath.Base(base)
		}
	}

	return ""
}

func extractTarGz(archivePath, destDir string) error {
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
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		name := stripArchiveRoot(header.Name)
		if name == "" {
			continue
		}

		target, err := archiveTargetPath(destDir, name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", target, err)
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
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create symlink parent for %s: %w", target, err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create symlink %s: %w", target, err)
			}
		}
	}
}

func extractZIP(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	for _, file := range reader.File {
		name := stripArchiveRoot(file.Name)
		if name == "" {
			continue
		}

		target, err := archiveTargetPath(destDir, name)
		if err != nil {
			return err
		}

		mode := file.Mode()
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, dirMode(mode)); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", target, err)
		}

		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", file.Name, err)
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

	return nil
}

func archiveTargetPath(destDir, name string) (string, error) {
	name = filepath.Clean(name)
	target := filepath.Join(destDir, name)
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", fmt.Errorf("resolve extracted path %s: %w", target, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", name)
	}
	return target, nil
}

func stripArchiveRoot(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return ""
	}
	if idx := strings.IndexByte(name, '/'); idx >= 0 {
		return strings.TrimPrefix(name[idx+1:], "/")
	}
	return name
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
