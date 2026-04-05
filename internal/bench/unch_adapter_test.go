package bench

import (
	"context"
	"os"
	"path/filepath"
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
