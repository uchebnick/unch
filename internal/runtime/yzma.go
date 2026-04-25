package runtime

import (
	"context"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const llamaBuilderReleasesAPI = "https://api.github.com/repos/hybridgroup/llama-cpp-builder/releases?per_page=10"

var (
	llamaLatestVersionFn  = fetchLatestLlamaVersion
	recentLlamaVersionsFn = recentLlamaVersions
	fallbackLlamaVersions = []string{"b8581", "b8580", "b8579", "b8578", "b8576"}
)

const (
	processorCPU    = "cpu"
	processorCUDA   = "cuda"
	processorMetal  = "metal"
	processorROCm   = "rocm"
	processorVulkan = "vulkan"
)

type YzmaResolver struct{}

// ResolveOrInstallYzmaLibPath resolves yzma shared libraries from --lib, YZMA_LIB,
// the local .semsearch runtime directory, or by downloading a managed runtime.
func (YzmaResolver) ResolveOrInstallYzmaLibPath(ctx context.Context, requestedPath string, localDir string, reporter Reporter) (string, string, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath != "" {
		return ResolveYzmaLibPath(requestedPath)
	}

	if envPath := strings.TrimSpace(os.Getenv("YZMA_LIB")); envPath != "" {
		if resolved, ok := validateYzmaLibDir(envPath); ok {
			return resolved, fmt.Sprintf("using YZMA_LIB=%s", resolved), nil
		}
	}

	installRoot := filepath.Join(localDir, "yzma")

	if resolved, ok := detectedYzmaLibDir(installRoot); ok {
		return resolved, fmt.Sprintf("using local yzma libs from %s", resolved), nil
	}

	if err := downloadYzmaLibraries(ctx, installRoot, reporter); err != nil {
		for _, candidate := range commonYzmaLibDirs() {
			if resolved, ok := validateYzmaLibDir(candidate); ok {
				return resolved, fmt.Sprintf("warning: automatic yzma install failed (%v); using %s", err, resolved), nil
			}
		}
		return "", "", fmt.Errorf("auto-install yzma libs: %w", err)
	}

	if resolved, ok := detectedYzmaLibDir(installRoot); ok {
		return resolved, fmt.Sprintf("downloaded yzma libs to %s", resolved), nil
	}

	return "", "", fmt.Errorf(
		"downloaded yzma libraries to %s, but required files are missing: %s",
		installRoot,
		strings.Join(requiredYzmaLibFiles(), ", "),
	)
}

func ResolveYzmaLibPath(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("empty yzma lib path")
	}

	if resolved, ok := validateYzmaLibDir(input); ok {
		return resolved, "", nil
	}

	if looksLikeDynamicLibraryPath(input) {
		if resolved, ok := validateYzmaLibDir(filepath.Dir(input)); ok {
			return resolved, "", nil
		}
	}

	if envPath := strings.TrimSpace(os.Getenv("YZMA_LIB")); envPath != "" && envPath != input {
		if resolved, ok := validateYzmaLibDir(envPath); ok {
			return resolved, fmt.Sprintf("warning: --lib=%s is not a valid yzma library location; using YZMA_LIB=%s", input, resolved), nil
		}
	}

	for _, candidate := range commonYzmaLibDirs() {
		if resolved, ok := validateYzmaLibDir(candidate); ok {
			return resolved, fmt.Sprintf("warning: --lib=%s is not a valid yzma library location; using %s", input, resolved), nil
		}
	}

	return "", "", fmt.Errorf(
		"--lib=%s is not a valid yzma library location; expected a directory containing %s or a path to one of those files",
		input,
		strings.Join(requiredYzmaLibFiles(), ", "),
	)
}

func downloadYzmaLibraries(ctx context.Context, installRoot string, reporter Reporter) error {
	parentDir := filepath.Dir(installRoot)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("create yzma install parent dir: %w", err)
	}

	versions, err := candidateLlamaVersions(ctx)
	if err != nil {
		return fmt.Errorf("detect candidate llama.cpp versions: %w", err)
	}

	processor := defaultYzmaProcessor()
	var lastErr error
	for _, version := range versions {
		stageRoot, err := os.MkdirTemp(parentDir, filepath.Base(installRoot)+".tmp-*")
		if err != nil {
			return fmt.Errorf("create yzma staging dir: %w", err)
		}

		if reporter != nil {
			reporter.Logf("downloading yzma libs: dst=%s os=%s arch=%s processor=%s version=%s", stageRoot, runtime.GOOS, runtime.GOARCH, processor, version)
		}

		progress := defaultProgressTracker
		if reporter != nil {
			progress = reporter.ProgressTracker("Downloading runtime")
		}

		err = downloadYzmaArchive(ctx, runtime.GOARCH, runtime.GOOS, processor, version, stageRoot, progress)
		if err == nil {
			if _, ok := detectedYzmaLibDir(stageRoot); !ok {
				_ = os.RemoveAll(stageRoot)
				lastErr = fmt.Errorf("downloaded files for %s, but required libraries were not found", version)
				continue
			}

			if err := replaceManagedDir(stageRoot, installRoot); err != nil {
				_ = os.RemoveAll(stageRoot)
				return fmt.Errorf("activate yzma install: %w", err)
			}
			return nil
		}

		_ = os.RemoveAll(stageRoot)
		if errors.Is(err, errYzmaArchiveNotFound) {
			lastErr = err
			continue
		}
		return fmt.Errorf("download yzma libs: %w", err)
	}

	if lastErr != nil {
		return fmt.Errorf("download yzma libs: %w", lastErr)
	}

	return fmt.Errorf("download yzma libs: no candidate versions available")
}

func defaultYzmaProcessor() string {
	if pinned := strings.TrimSpace(os.Getenv("SEMSEARCH_YZMA_PROCESSOR")); pinned != "" {
		return pinned
	}

	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return processorMetal
		}
		return processorCPU
	case "linux":
		if ok, _ := hasCUDA(); ok {
			return processorCUDA
		}
		if ok, _ := hasROCm(); ok {
			return processorROCm
		}
		return processorCPU
	default:
		return processorCPU
	}
}

func validateYzmaLibDir(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}

	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)

	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", false
	}

	for _, filename := range requiredYzmaLibFiles() {
		if _, err := os.Stat(filepath.Join(resolved, filename)); err != nil {
			return "", false
		}
	}
	if !yzmaLibDirHasRequiredSymbols(resolved) {
		return "", false
	}

	return resolved, true
}

func detectedYzmaLibDir(installRoot string) (string, bool) {
	candidates := []string{
		installRoot,
		filepath.Join(installRoot, "lib"),
	}

	for _, candidate := range candidates {
		if resolved, ok := validateYzmaLibDir(candidate); ok {
			return resolved, true
		}
	}

	return "", false
}

func requiredYzmaLibFiles() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"ggml.dll", "ggml-base.dll", "llama.dll"}
	case "linux", "freebsd":
		return []string{"libggml.so", "libggml-base.so", "libllama.so"}
	default:
		return []string{"libggml.dylib", "libggml-base.dylib", "libllama.dylib"}
	}
}

func yzmaLibDirHasRequiredSymbols(dir string) bool {
	libPath := filepath.Join(dir, llamaLibraryFilename())
	hasSymbol, checked := dynamicLibraryExportsSymbol(libPath, "llama_params_fit")
	if !checked {
		// Unit tests and unusual platforms may use placeholder files. Keep the
		// structural validation above as the fallback when the binary format is
		// not inspectable.
		return true
	}
	return hasSymbol
}

func llamaLibraryFilename() string {
	switch runtime.GOOS {
	case "windows":
		return "llama.dll"
	case "linux", "freebsd":
		return "libllama.so"
	default:
		return "libllama.dylib"
	}
}

func dynamicLibraryExportsSymbol(path string, symbol string) (bool, bool) {
	switch runtime.GOOS {
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return false, false
		}
		defer func() {
			_ = file.Close()
		}()
		if file.Symtab == nil {
			return false, false
		}
		for _, sym := range file.Symtab.Syms {
			if sym.Name == symbol || sym.Name == "_"+symbol {
				return true, true
			}
		}
		return false, true
	case "linux", "freebsd":
		file, err := elf.Open(path)
		if err != nil {
			return false, false
		}
		defer func() {
			_ = file.Close()
		}()
		symbols, err := file.DynamicSymbols()
		if err != nil {
			symbols, err = file.Symbols()
		}
		if err != nil {
			return false, false
		}
		for _, sym := range symbols {
			if sym.Name == symbol {
				return true, true
			}
		}
		return false, true
	default:
		return false, false
	}
}

func looksLikeDynamicLibraryPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	switch runtime.GOOS {
	case "windows":
		return strings.HasSuffix(path, ".dll")
	case "linux", "freebsd":
		return strings.HasSuffix(path, ".so")
	default:
		return strings.HasSuffix(path, ".dylib")
	}
}

func commonYzmaLibDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	return []string{
		filepath.Join(home, ".docker", "bin", "lib"),
	}
}

func EnsureDynamicLibraryLookupPath(libDir string) error {
	libDir = strings.TrimSpace(libDir)
	if libDir == "" {
		return nil
	}

	envVar := dynamicLibraryLookupEnvVar()
	if envVar == "" {
		return nil
	}

	current := os.Getenv(envVar)
	for _, entry := range filepath.SplitList(current) {
		if filepath.Clean(entry) == filepath.Clean(libDir) {
			return nil
		}
	}

	if current == "" {
		return os.Setenv(envVar, libDir)
	}

	return os.Setenv(envVar, libDir+string(os.PathListSeparator)+current)
}

func dynamicLibraryLookupEnvVar() string {
	switch runtime.GOOS {
	case "darwin":
		return "DYLD_LIBRARY_PATH"
	case "linux", "freebsd":
		return "LD_LIBRARY_PATH"
	case "windows":
		return "PATH"
	default:
		return ""
	}
}

func replaceManagedDir(src string, dst string) error {
	backup := dst + ".bak"
	_ = os.RemoveAll(backup)

	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backup); err != nil {
			return fmt.Errorf("move current dir to backup: %w", err)
		}
	}

	if err := os.Rename(src, dst); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, dst)
		}
		return fmt.Errorf("move staged dir into place: %w", err)
	}

	_ = os.RemoveAll(backup)
	return nil
}

func candidateLlamaVersions(ctx context.Context) ([]string, error) {
	var versions []string
	seen := make(map[string]struct{})

	add := func(version string) {
		version = strings.TrimSpace(version)
		if version == "" {
			return
		}
		if _, exists := seen[version]; exists {
			return
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}

	if pinned := strings.TrimSpace(os.Getenv("SEMSEARCH_YZMA_VERSION")); pinned != "" {
		add(pinned)
		return versions, nil
	}

	latest, err := llamaLatestVersionFn()
	if err == nil {
		add(latest)
	}

	recent, recentErr := recentLlamaVersionsFn(ctx)
	for _, version := range recent {
		add(version)
	}

	if len(versions) == 0 {
		for _, version := range fallbackLlamaVersions {
			add(version)
		}
	}

	if len(versions) == 0 {
		if err != nil {
			return nil, err
		}
		if recentErr != nil {
			return nil, recentErr
		}
		return nil, fmt.Errorf("no llama.cpp versions found")
	}

	return versions, nil
}

func recentLlamaVersions(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llamaBuilderReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("github releases api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		if release.TagName != "" {
			versions = append(versions, release.TagName)
		}
	}

	return versions, nil
}
