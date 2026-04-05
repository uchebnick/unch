package bench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	appruntime "github.com/uchebnick/unch/internal/runtime"
)

var (
	searchHitPattern    = regexp.MustCompile(`(?m)^\s*(\d+)\.\s+(.+):(\d+)\s{2,}.*$`)
	indexSummaryPattern = regexp.MustCompile(`Indexed\s+(\d+)\s+symbols\s+in\s+(\d+)\s+files`)
)

const indexUpToDateSummary = "Index already up to date"

const unchBuildPackage = "./cmd/unch"

type UnchAdapter struct {
	binaryPath string
	version    string
	modelPath  string
	libPath    string
}

func (a *UnchAdapter) Name() string {
	return "unch"
}

func (a *UnchAdapter) Version() string {
	if strings.TrimSpace(a.version) == "" {
		return "unknown"
	}
	return a.version
}

func (a *UnchAdapter) Prepare(ctx context.Context, env Environment) error {
	if err := validateUnchToolOptions(env.ToolOptions); err != nil {
		return err
	}

	if err := os.MkdirAll(env.BinDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	if err := os.MkdirAll(env.WarmupRoot, 0o755); err != nil {
		return fmt.Errorf("create warmup dir: %w", err)
	}

	a.binaryPath = filepath.Join(env.BinDir, benchmarkBinaryName(env.OS))
	if err := a.buildBinary(ctx, env); err != nil {
		return err
	}

	a.version = gitDescribe(ctx, env.RepoRoot)

	defaultModelPath := appruntime.DefaultModelPath(filepath.Join(env.SemsearchHome, "models"))
	if toolModel := strings.TrimSpace(env.ToolOptions["model"]); toolModel != "" {
		resolvedModelPath, err := appruntime.CanonicalModelPath(toolModel, defaultModelPath)
		if err != nil {
			return fmt.Errorf("resolve benchmark model path: %w", err)
		}
		a.modelPath = resolvedModelPath
	} else {
		a.modelPath = defaultModelPath
	}
	if toolLib := strings.TrimSpace(env.ToolOptions["lib"]); toolLib != "" {
		a.libPath = toolLib
	}

	if err := writeWarmupProgram(env.WarmupRoot); err != nil {
		return fmt.Errorf("write warmup program: %w", err)
	}

	args := []string{"index", "--root", env.WarmupRoot}
	if strings.TrimSpace(env.ToolOptions["model"]) != "" {
		args = append(args, "--model", a.modelPath)
	}
	if a.libPath != "" {
		args = append(args, "--lib", a.libPath)
	}

	if _, _, _, err := a.runCommand(ctx, env, args...); err != nil {
		return fmt.Errorf("warm runtime and model caches: %w", err)
	}

	if a.libPath == "" {
		resolvedLibPath, err := discoverManagedYzmaLibDir(env.WarmupRoot)
		if err != nil {
			return fmt.Errorf("discover warmed yzma libs: %w", err)
		}
		a.libPath = resolvedLibPath
	}

	if _, err := os.Stat(a.modelPath); err != nil {
		return fmt.Errorf("discover warmed default model at %s: %w", a.modelPath, err)
	}

	return nil
}

func (a *UnchAdapter) Index(ctx context.Context, repo CheckedOutRepo, env Environment, cfg RunConfig) (IndexRunResult, error) {
	args := []string{
		"index",
		"--root", repo.Root,
		"--model", a.modelPath,
		"--lib", a.libPath,
	}

	_, combined, duration, err := a.runCommand(ctx, env, args...)
	if err != nil {
		return IndexRunResult{}, fmt.Errorf("index %s: %w", repo.Case.ID, err)
	}

	summary, indexedSymbols, indexedFiles, err := parseIndexSummary(combined)
	if err != nil {
		return IndexRunResult{}, fmt.Errorf("parse index summary for %s: %w", repo.Case.ID, err)
	}

	return IndexRunResult{
		Summary:        summary,
		IndexedSymbols: indexedSymbols,
		IndexedFiles:   indexedFiles,
		Duration:       duration,
	}, nil
}

func (a *UnchAdapter) Search(ctx context.Context, repo CheckedOutRepo, query QueryCase, env Environment, cfg RunConfig) (SearchRunResult, error) {
	args := []string{
		"search",
		"--root", repo.Root,
		"--limit", strconv.Itoa(cfg.SearchLimit),
		"--mode", query.Mode,
		"--model", a.modelPath,
		"--lib", a.libPath,
		query.Text,
	}

	stdout, combined, duration, err := a.runCommand(ctx, env, args...)
	if err != nil {
		return SearchRunResult{}, fmt.Errorf("search %s/%s: %w", repo.Case.ID, query.ID, err)
	}

	hits, err := parseSearchHits(stdout, combined)
	if err != nil {
		return SearchRunResult{}, fmt.Errorf("parse search hits for %s/%s: %w", repo.Case.ID, query.ID, err)
	}

	return SearchRunResult{
		Hits:     hits,
		Duration: duration,
	}, nil
}

func (a *UnchAdapter) buildBinary(ctx context.Context, env Environment) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", a.binaryPath, unchBuildPackage)
	cmd.Dir = env.RepoRoot
	cmd.Env = a.commandEnv(env)

	var stderr bytes.Buffer
	cmd.Stdout = ioDiscard{}
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build unch benchmark binary: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (a *UnchAdapter) runCommand(ctx context.Context, env Environment, args ...string) (string, string, time.Duration, error) {
	cmd := exec.CommandContext(ctx, a.binaryPath, args...)
	cmd.Env = a.commandEnv(env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	combined := stderr.String() + stdout.String()
	if err != nil {
		return stdout.String(), combined, duration, fmt.Errorf("%w: %s", err, strings.TrimSpace(combined))
	}

	return stdout.String(), combined, duration, nil
}

func (a *UnchAdapter) commandEnv(env Environment) []string {
	base := os.Environ()
	base = append(base,
		"SEMSEARCH_HOME="+env.SemsearchHome,
		"GOCACHE="+filepath.Join(env.CacheRoot, "gocache"),
		"GOMODCACHE="+filepath.Join(env.CacheRoot, "gomodcache"),
		"NO_COLOR=1",
		"TERM=dumb",
	)
	return base
}

func validateUnchToolOptions(options map[string]string) error {
	for key := range options {
		switch key {
		case "model", "lib":
		default:
			return fmt.Errorf("unsupported unch tool option %q", key)
		}
	}
	return nil
}

func parseSearchHits(stdout string, combined string) ([]SearchHit, error) {
	matches := searchHitPattern.FindAllStringSubmatch(stdout, -1)
	if len(matches) == 0 {
		if strings.Contains(combined, "No matches found") {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected search output: %q", strings.TrimSpace(stdout))
	}

	hits := make([]SearchHit, 0, len(matches))
	for _, match := range matches {
		rank, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse rank %q: %w", match[1], err)
		}
		line, err := strconv.Atoi(match[3])
		if err != nil {
			return nil, fmt.Errorf("parse line %q: %w", match[3], err)
		}
		hits = append(hits, SearchHit{
			Rank: rank,
			Path: strings.TrimSpace(match[2]),
			Line: line,
		})
	}

	return hits, nil
}

func parseIndexSummary(output string) (string, int, int, error) {
	match := indexSummaryPattern.FindStringSubmatch(output)
	if len(match) == 3 {
		indexedSymbols, err := strconv.Atoi(match[1])
		if err != nil {
			return "", 0, 0, fmt.Errorf("parse indexed symbols %q: %w", match[1], err)
		}
		indexedFiles, err := strconv.Atoi(match[2])
		if err != nil {
			return "", 0, 0, fmt.Errorf("parse indexed files %q: %w", match[2], err)
		}

		return match[0], indexedSymbols, indexedFiles, nil
	}

	if strings.Contains(output, indexUpToDateSummary) {
		return indexUpToDateSummary, 0, 0, nil
	}

	return "", 0, 0, fmt.Errorf("indexed summary not found in output")
}

func benchmarkBinaryName(goos string) string {
	if goos == "windows" {
		return "unch.exe"
	}
	return "unch"
}

func writeWarmupProgram(root string) error {
	content := "package warmup\n\n// Hello returns a small symbol for benchmark cache warmup.\nfunc Hello() string { return \"hello\" }\n"
	return os.WriteFile(filepath.Join(root, "warmup.go"), []byte(content), 0o644)
}

func gitDescribe(ctx context.Context, repoRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "describe", "--tags", "--always", "--dirty")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func discoverManagedYzmaLibDir(warmupRoot string) (string, error) {
	installRoot := filepath.Join(warmupRoot, ".semsearch", "yzma")
	candidates := []string{
		installRoot,
		filepath.Join(installRoot, "lib"),
	}

	requiredFiles := requiredYzmaLibFilesForGOOS(runtime.GOOS)
	for _, candidate := range candidates {
		ok := true
		for _, filename := range requiredFiles {
			if _, err := os.Stat(filepath.Join(candidate, filename)); err != nil {
				ok = false
				break
			}
		}
		if ok {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("yzma libs not found under %s", installRoot)
}

func requiredYzmaLibFilesForGOOS(goos string) []string {
	switch goos {
	case "windows":
		return []string{"ggml.dll", "ggml-base.dll", "llama.dll"}
	case "linux", "freebsd":
		return []string{"libggml.so", "libggml-base.so", "libllama.so"}
	default:
		return []string{"libggml.dylib", "libggml-base.dylib", "libllama.dylib"}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
