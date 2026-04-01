package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if repoReport.Metrics.Top1 != 0.5 || repoReport.Metrics.Top3 != 1 || repoReport.Metrics.MRR != 0.75 || repoReport.Metrics.QualityScore != 68 {
		t.Fatalf("unexpected repo metrics: %+v", repoReport.Metrics)
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
