package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAdapter struct {
	indexCalls  int
	searchCalls map[string]int
}

func (f *fakeAdapter) Name() string    { return "fake" }
func (f *fakeAdapter) Version() string { return "fake-v1" }

func (f *fakeAdapter) Prepare(ctx context.Context, env Environment) error {
	if f.searchCalls == nil {
		f.searchCalls = make(map[string]int)
	}
	return nil
}

func (f *fakeAdapter) Index(ctx context.Context, repo CheckedOutRepo, env Environment, cfg RunConfig) (IndexRunResult, error) {
	f.indexCalls++
	indexPath := filepath.Join(repo.Root, ".semsearch", "index.db")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return IndexRunResult{}, err
	}
	if err := os.WriteFile(indexPath, []byte("ok"), 0o644); err != nil {
		return IndexRunResult{}, err
	}

	switch f.indexCalls {
	case 1:
		return IndexRunResult{Summary: "Indexed 10 symbols in 3 files", IndexedSymbols: 10, IndexedFiles: 3, Duration: 100 * time.Millisecond}, nil
	case 2:
		return IndexRunResult{Summary: "Indexed 10 symbols in 3 files", IndexedSymbols: 10, IndexedFiles: 3, Duration: 200 * time.Millisecond}, nil
	default:
		return IndexRunResult{Summary: "Indexed 10 symbols in 3 files", IndexedSymbols: 10, IndexedFiles: 3, Duration: 300 * time.Millisecond}, nil
	}
}

func (f *fakeAdapter) Search(ctx context.Context, repo CheckedOutRepo, query QueryCase, env Environment, cfg RunConfig) (SearchRunResult, error) {
	key := repo.Case.ID + "/" + query.ID
	f.searchCalls[key]++

	switch query.ID {
	case "exact":
		return SearchRunResult{
			Duration: 50 * time.Millisecond,
			Hits: []SearchHit{
				{Rank: 1, Path: "main.go", Line: 12},
			},
		}, nil
	default:
		return SearchRunResult{
			Duration: 80 * time.Millisecond,
			Hits: []SearchHit{
				{Rank: 2, Path: "main.go", Line: 20},
			},
		}, nil
	}
}

func TestRunBenchmarkProducesStableReport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	repoRoot := createGitRepo(t, root)
	suite := Suite{
		ID:      "test-suite",
		Version: 1,
		Name:    "test-suite",
		Repositories: []RepositoryCase{
			{
				ID:       "local/repo",
				URL:      repoRoot,
				Commit:   gitHead(t, repoRoot),
				Language: "go",
				Queries: []QueryCase{
					{ID: "exact", Text: "find main", Mode: "auto", ExpectedHits: []string{"main.go:12"}},
					{ID: "second", Text: "other", Mode: "lexical", ExpectedHits: []string{"main.go:20"}},
				},
			},
		},
	}

	suitePath := filepath.Join(root, "suite.json")
	suiteJSON, err := json.Marshal(suite)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := os.WriteFile(suitePath, suiteJSON, 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	suiteRevision, err := SuiteRevision(suitePath)
	if err != nil {
		t.Fatalf("SuiteRevision() error: %v", err)
	}

	env, err := NewEnvironment(root, suitePath, filepath.Join(root, "bench"), filepath.Join(root, "results"), nil)
	if err != nil {
		t.Fatalf("NewEnvironment() error: %v", err)
	}

	adapter := &fakeAdapter{}
	report, err := RunBenchmark(ctx, adapter, suite, env, RunConfig{
		ColdIndexRuns:  1,
		WarmIndexRuns:  2,
		WarmSearchRuns: 2,
		SearchLimit:    10,
	}, io.Discard)
	if err != nil {
		t.Fatalf("RunBenchmark() error: %v", err)
	}

	if report.Tool != "fake" {
		t.Fatalf("report.Tool = %q", report.Tool)
	}
	if report.SuiteRevision != suiteRevision {
		t.Fatalf("report.SuiteRevision = %q, want %q", report.SuiteRevision, suiteRevision)
	}
	if len(report.Repositories) != 1 {
		t.Fatalf("report.Repositories = %d", len(report.Repositories))
	}
	repoReport := report.Repositories[0]
	if repoReport.Timing.ColdIndexMeanMS != 100 {
		t.Fatalf("cold index mean = %v", repoReport.Timing.ColdIndexMeanMS)
	}
	if repoReport.Timing.WarmIndexMeanMS != 250 {
		t.Fatalf("warm index mean = %v", repoReport.Timing.WarmIndexMeanMS)
	}
	if repoReport.Timing.WarmSearchMeanMS != 65 {
		t.Fatalf("warm search mean = %v", repoReport.Timing.WarmSearchMeanMS)
	}
	if report.Coverage.RepositoryCount != 1 || report.Coverage.QueryCount != 2 {
		t.Fatalf("unexpected suite coverage: %+v", report.Coverage)
	}
	if repoReport.Stats.QueryCount != 2 {
		t.Fatalf("repoReport.Stats.QueryCount = %d", repoReport.Stats.QueryCount)
	}
	if repoReport.Stats.LastIndexedSymbols != 10 || repoReport.Stats.LastIndexedFiles != 3 {
		t.Fatalf("unexpected repo stats: %+v", repoReport.Stats)
	}
	if repoReport.Metrics.Top1 != 0.5 || repoReport.Metrics.Top3 != 1 || repoReport.Metrics.MRR != 0.75 || repoReport.Metrics.QualityScore != 68 {
		t.Fatalf("unexpected repo metrics: %+v", repoReport.Metrics)
	}
	if len(repoReport.Queries) != 2 {
		t.Fatalf("repoReport.Queries = %d", len(repoReport.Queries))
	}
	if repoReport.Queries[0].Timing.WarmSearchMeanMS != 50 {
		t.Fatalf("first query mean = %v", repoReport.Queries[0].Timing.WarmSearchMeanMS)
	}
	if repoReport.Queries[0].TopHit == nil || repoReport.Queries[0].TopHit.Path != "main.go" || repoReport.Queries[0].TopHit.Line != 12 {
		t.Fatalf("first query top hit = %+v", repoReport.Queries[0].TopHit)
	}
	if repoReport.Queries[1].Metrics.ObservedRank != 2 {
		t.Fatalf("second query observed rank = %d", repoReport.Queries[1].Metrics.ObservedRank)
	}

	outputPath := filepath.Join(root, "results", "report.json")
	if err := WriteReportJSON(outputPath, report); err != nil {
		t.Fatalf("WriteReportJSON() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if decoded.Metrics.QualityScore != 68 {
		t.Fatalf("decoded quality score = %d", decoded.Metrics.QualityScore)
	}
	if decoded.Coverage.QueryCount != 2 {
		t.Fatalf("decoded coverage query count = %d", decoded.Coverage.QueryCount)
	}
}

type cacheAwareAdapter struct {
	indexSawExisting []bool
}

func (a *cacheAwareAdapter) Name() string    { return "cache-aware" }
func (a *cacheAwareAdapter) Version() string { return "cache-aware-v1" }
func (a *cacheAwareAdapter) Prepare(ctx context.Context, env Environment) error {
	return nil
}
func (a *cacheAwareAdapter) Index(ctx context.Context, repo CheckedOutRepo, env Environment, cfg RunConfig) (IndexRunResult, error) {
	indexPath := filepath.Join(repo.Root, ".semsearch", "index.db")
	_, err := os.Stat(indexPath)
	a.indexSawExisting = append(a.indexSawExisting, err == nil)

	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return IndexRunResult{}, err
	}
	if err := os.WriteFile(indexPath, []byte("ok"), 0o644); err != nil {
		return IndexRunResult{}, err
	}

	return IndexRunResult{
		Summary:        "Indexed 1 symbols in 1 files",
		IndexedSymbols: 1,
		IndexedFiles:   1,
		Duration:       10 * time.Millisecond,
	}, nil
}
func (a *cacheAwareAdapter) Search(ctx context.Context, repo CheckedOutRepo, query QueryCase, env Environment, cfg RunConfig) (SearchRunResult, error) {
	return SearchRunResult{
		Duration: 5 * time.Millisecond,
		Hits:     []SearchHit{{Rank: 1, Path: "main.go", Line: 12}},
	}, nil
}

func TestRunBenchmarkWarmIndexReusesExistingLocalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	repoRoot := createGitRepo(t, root)
	suite := Suite{
		ID:      "warm-cache-suite",
		Version: 1,
		Name:    "warm-cache-suite",
		Repositories: []RepositoryCase{
			{
				ID:       "local/repo",
				URL:      repoRoot,
				Commit:   gitHead(t, repoRoot),
				Language: "go",
				Queries: []QueryCase{
					{ID: "exact", Text: "find main", Mode: "auto", ExpectedHits: []string{"main.go:12"}},
				},
			},
		},
	}

	suitePath := filepath.Join(root, "suite.json")
	suiteJSON, err := json.Marshal(suite)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := os.WriteFile(suitePath, suiteJSON, 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	env, err := NewEnvironment(root, suitePath, filepath.Join(root, "bench"), filepath.Join(root, "results"), nil)
	if err != nil {
		t.Fatalf("NewEnvironment() error: %v", err)
	}

	adapter := &cacheAwareAdapter{}
	if _, err := RunBenchmark(ctx, adapter, suite, env, RunConfig{
		ColdIndexRuns:  1,
		WarmIndexRuns:  2,
		WarmSearchRuns: 1,
		SearchLimit:    10,
	}, io.Discard); err != nil {
		t.Fatalf("RunBenchmark() error: %v", err)
	}

	if len(adapter.indexSawExisting) != 3 {
		t.Fatalf("index calls = %d, want 3", len(adapter.indexSawExisting))
	}
	if adapter.indexSawExisting[0] {
		t.Fatalf("cold index unexpectedly saw an existing local index")
	}
	if !adapter.indexSawExisting[1] || !adapter.indexSawExisting[2] {
		t.Fatalf("warm index runs should reuse existing local state, got %v", adapter.indexSawExisting)
	}
}

func TestPrintSummaryIncludesCoverageProfileAndMisses(t *testing.T) {
	t.Parallel()

	report := Report{
		Tool:          "fake",
		SuitePath:     "/tmp/suite.json",
		SuiteRevision: "sha256:test",
		Suite:         Suite{ID: "default", Version: 3},
		Coverage: SuiteCoverage{
			RepositoryCount: 1,
			QueryCount:      2,
			ModeCounts:      map[string]int{"auto": 1, "lexical": 1},
		},
		Environment: ReportEnvironment{
			OS:          "darwin",
			Arch:        "arm64",
			CPUInfo:     "Apple M4",
			NumCPU:      10,
			ToolVersion: "fake-v1",
		},
		Config: RunConfig{
			ColdIndexRuns:  1,
			WarmIndexRuns:  2,
			WarmSearchRuns: 3,
			SearchLimit:    10,
		},
		Timing:  TimingMetrics{ColdIndexMeanMS: 1000, WarmIndexMeanMS: 250, WarmSearchMeanMS: 65},
		Metrics: AggregateMetrics{QualityScore: 68, Top1: 0.5, Top3: 1, MRR: 0.75},
		Repositories: []RepositoryReport{
			{
				ID:       "local/repo",
				Language: "go",
				Commit:   "1234567890abcdef1234567890abcdef12345678",
				Stats: RepositoryStats{
					QueryCount:         2,
					ModeCounts:         map[string]int{"auto": 1, "lexical": 1},
					LastIndexedSymbols: 10,
					LastIndexedFiles:   3,
				},
				Timing:  TimingMetrics{ColdIndexMeanMS: 1000, WarmIndexMeanMS: 250, WarmSearchMeanMS: 65},
				Metrics: AggregateMetrics{QualityScore: 68, Top1: 0.5, Top3: 1, MRR: 0.75},
				Queries: []QueryReport{
					{
						ID:           "exact",
						Mode:         "auto",
						ExpectedHits: []string{"main.go:12"},
						Timing:       QueryTiming{WarmSearchMeanMS: 50},
						TopHit:       &SearchHit{Rank: 1, Path: "main.go", Line: 12},
						Metrics:      QueryMetrics{Top1Success: true, Top3Success: true, RR: 1, ObservedRank: 1},
					},
					{
						ID:           "second",
						Mode:         "lexical",
						ExpectedHits: []string{"main.go:20"},
						Timing:       QueryTiming{WarmSearchMeanMS: 80},
						TopHit:       &SearchHit{Rank: 2, Path: "main.go", Line: 20},
						Metrics:      QueryMetrics{Top1Success: false, Top3Success: true, RR: 0.5, ObservedRank: 2},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := PrintSummary(&buf, report); err != nil {
		t.Fatalf("PrintSummary() error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"Suite coverage: 1 repos • 2 queries • auto=1 lexical=1",
		"Run profile: 1 cold / 2 warm / 3 search repeats • top 10 hits",
		"latest index snapshot: 10 symbols / 3 files",
		"Top1 misses: 1",
		"local/repo / second (lexical, 80.00ms): expected main.go:20, got main.go:20 @ rank 2",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("PrintSummary() output missing %q:\n%s", want, output)
		}
	}
}

func createGitRepo(t *testing.T, root string) string {
	t.Helper()

	repoRoot := filepath.Join(root, "fixture")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	runGitCommand(t, repoRoot, "init")
	runGitCommand(t, repoRoot, "add", ".")
	runGitCommand(t, repoRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return repoRoot
}

func gitHead(t *testing.T, repoRoot string) string {
	t.Helper()

	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD error: %v", err)
	}
	return string(bytes.TrimSpace(output))
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v error: %v\n%s", args, err, string(output))
	}
}
