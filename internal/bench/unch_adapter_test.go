package bench

import (
	"context"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"testing"
)

func TestParseSearchHits(t *testing.T) {
	t.Parallel()

	stdout := " 1. mux.go:32  0.7747\n 2. route.go:177  0.8123\n"
	hits, err := parseSearchHits(stdout, stdout)
	if err != nil {
		t.Fatalf("parseSearchHits() error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("parseSearchHits() hits = %d", len(hits))
	}
	if hits[0].Rank != 1 || hits[0].Path != "mux.go" || hits[0].Line != 32 {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}
}

func TestParseSearchHitsNoMatches(t *testing.T) {
	t.Parallel()

	hits, err := parseSearchHits("", "Loaded model       dim=768\nNo matches found\n")
	if err != nil {
		t.Fatalf("parseSearchHits() error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("parseSearchHits() hits = %+v, want empty", hits)
	}
}

func TestParseSearchHitsRejectsUnexpectedOutput(t *testing.T) {
	t.Parallel()

	_, err := parseSearchHits("Loaded model dim=768\nweird output\n", "Loaded model dim=768\nweird output\n")
	if err == nil {
		t.Fatalf("parseSearchHits() error = nil, want parse failure")
	}
}

func TestParseIndexSummary(t *testing.T) {
	t.Parallel()

	summary, indexedSymbols, indexedFiles, err := parseIndexSummary("Loaded model dim=768\nIndexed 278 symbols in 16 files\n")
	if err != nil {
		t.Fatalf("parseIndexSummary() error: %v", err)
	}
	if summary != "Indexed 278 symbols in 16 files" {
		t.Fatalf("parseIndexSummary() summary = %q", summary)
	}
	if indexedSymbols != 278 || indexedFiles != 16 {
		t.Fatalf("parseIndexSummary() = (%d, %d)", indexedSymbols, indexedFiles)
	}
}

func TestParseIndexSummaryUpToDate(t *testing.T) {
	t.Parallel()

	summary, indexedSymbols, indexedFiles, err := parseIndexSummary("Loaded model dim=768\nIndex already up to date\n")
	if err != nil {
		t.Fatalf("parseIndexSummary() error: %v", err)
	}
	if summary != indexUpToDateSummary {
		t.Fatalf("parseIndexSummary() summary = %q", summary)
	}
	if indexedSymbols != 0 || indexedFiles != 0 {
		t.Fatalf("parseIndexSummary() = (%d, %d)", indexedSymbols, indexedFiles)
	}
}

func TestBenchmarkBinaryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: "unch"},
		{goos: "linux", want: "unch"},
		{goos: "windows", want: "unch.exe"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.goos, func(t *testing.T) {
			t.Parallel()
			if got := benchmarkBinaryName(tt.goos); got != tt.want {
				t.Fatalf("benchmarkBinaryName(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestUnchAdapterBuildBinaryFromCmdUnch(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	tempRoot := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(tempRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			mode := os.FileMode(0o644)
			if info.IsDir() {
				mode = 0o755
			}
			_ = os.Chmod(path, mode)
			return nil
		})
	})

	env, err := NewEnvironment(
		repoRoot,
		filepath.Join(tempRoot, "suite.json"),
		filepath.Join(tempRoot, "bench"),
		filepath.Join(tempRoot, "results"),
		nil,
	)
	if err != nil {
		t.Fatalf("NewEnvironment() error: %v", err)
	}

	adapter := &UnchAdapter{
		binaryPath: filepath.Join(env.BinDir, benchmarkBinaryName(env.OS)),
	}
	if err := adapter.buildBinary(context.Background(), env); err != nil {
		t.Fatalf("buildBinary() error: %v", err)
	}

	info, err := os.Stat(adapter.binaryPath)
	if err != nil {
		t.Fatalf("stat built binary: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("built binary path is a directory: %s", adapter.binaryPath)
	}
}

func TestDiscoverWarmedYzmaLibDirUsesLoggedPath(t *testing.T) {
	t.Parallel()

	warmupRoot := t.TempDir()
	cachedDir := filepath.Join(t.TempDir(), "cached-yzma")
	writeRequiredYzmaFiles(t, cachedDir)

	logPath := filepath.Join(warmupRoot, ".semsearch", "logs", "run.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(logPath, []byte("2026/04/06 09:44:10 lib="+cachedDir+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := discoverWarmedYzmaLibDir(warmupRoot)
	if err != nil {
		t.Fatalf("discoverWarmedYzmaLibDir() error = %v", err)
	}
	if got != filepath.Clean(cachedDir) {
		t.Fatalf("discoverWarmedYzmaLibDir() = %q, want %q", got, cachedDir)
	}
}

func TestDiscoverWarmedYzmaLibDirIgnoresLegacyLocalRuntime(t *testing.T) {
	t.Parallel()

	warmupRoot := t.TempDir()
	legacyDir := filepath.Join(warmupRoot, ".semsearch", "yzma", "lib")
	writeRequiredYzmaFiles(t, legacyDir)

	if _, err := discoverWarmedYzmaLibDir(warmupRoot); err == nil {
		t.Fatalf("discoverWarmedYzmaLibDir() unexpectedly accepted legacy local runtime")
	}
}

func writeRequiredYzmaFiles(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", dir, err)
	}
	for _, name := range requiredYzmaLibFilesForGOOS(stdruntime.GOOS) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ok"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
}
