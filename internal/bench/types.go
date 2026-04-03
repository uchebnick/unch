package bench

import (
	"context"
	"time"
)

type Suite struct {
	ID           string           `json:"suite_id"`
	Version      int              `json:"suite_version"`
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	Repositories []RepositoryCase `json:"repositories"`
}

type RepositoryCase struct {
	ID       string      `json:"id"`
	URL      string      `json:"url"`
	Commit   string      `json:"commit"`
	Language string      `json:"language"`
	Queries  []QueryCase `json:"queries"`
}

type QueryCase struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Mode         string   `json:"mode"`
	ExpectedHits []string `json:"expected_hits"`
}

type Environment struct {
	RepoRoot      string
	SuitePath     string
	BenchRoot     string
	CacheRoot     string
	ReposRoot     string
	BinDir        string
	ResultsDir    string
	SemsearchHome string
	WarmupRoot    string
	OS            string
	Arch          string
	CPUInfo       string
	NumCPU        int
	ToolOptions   map[string]string
}

type RunConfig struct {
	ColdIndexRuns  int
	WarmIndexRuns  int
	WarmSearchRuns int
	SearchLimit    int
}

type CheckedOutRepo struct {
	Case RepositoryCase
	Root string
}

type SearchHit struct {
	Rank int    `json:"rank"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

func (h SearchHit) ExactRef() string {
	return exactRef(h.Path, h.Line)
}

type IndexRunResult struct {
	Summary        string
	IndexedSymbols int
	IndexedFiles   int
	Duration       time.Duration
}

type SearchRunResult struct {
	Hits     []SearchHit
	Duration time.Duration
}

type Adapter interface {
	Name() string
	Version() string
	Prepare(ctx context.Context, env Environment) error
	Index(ctx context.Context, repo CheckedOutRepo, env Environment, cfg RunConfig) (IndexRunResult, error)
	Search(ctx context.Context, repo CheckedOutRepo, query QueryCase, env Environment, cfg RunConfig) (SearchRunResult, error)
}

type QueryMetrics struct {
	Top1Success  bool    `json:"top1_success"`
	Top3Success  bool    `json:"top3_success"`
	RR           float64 `json:"rr"`
	ObservedRank int     `json:"observed_rank,omitempty"`
}

type AggregateMetrics struct {
	Top1         float64 `json:"top1"`
	Top3         float64 `json:"top3"`
	MRR          float64 `json:"mrr"`
	QualityScore int     `json:"quality_score"`
}

type TimingMetrics struct {
	ColdIndexMeanMS  float64 `json:"cold_index_mean_ms"`
	WarmIndexMeanMS  float64 `json:"warm_index_mean_ms"`
	WarmSearchMeanMS float64 `json:"warm_search_mean_ms"`
}

type QueryTiming struct {
	WarmSearchMeanMS float64 `json:"warm_search_mean_ms"`
}

type SuiteCoverage struct {
	RepositoryCount int            `json:"repository_count"`
	QueryCount      int            `json:"query_count"`
	ModeCounts      map[string]int `json:"mode_counts,omitempty"`
}

type RepositoryStats struct {
	QueryCount         int            `json:"query_count"`
	ModeCounts         map[string]int `json:"mode_counts,omitempty"`
	LastIndexedSymbols int            `json:"last_indexed_symbols,omitempty"`
	LastIndexedFiles   int            `json:"last_indexed_files,omitempty"`
}

type SearchRunReport struct {
	DurationMS float64     `json:"duration_ms"`
	Hits       []SearchHit `json:"hits"`
}

type IndexRunReport struct {
	Summary        string  `json:"summary"`
	IndexedSymbols int     `json:"indexed_symbols"`
	IndexedFiles   int     `json:"indexed_files"`
	DurationMS     float64 `json:"duration_ms"`
}

type QueryReport struct {
	ID           string            `json:"id"`
	Text         string            `json:"text"`
	Mode         string            `json:"mode"`
	ExpectedHits []string          `json:"expected_hits"`
	Runs         []SearchRunReport `json:"runs"`
	Timing       QueryTiming       `json:"timing"`
	TopHit       *SearchHit        `json:"top_hit,omitempty"`
	Metrics      QueryMetrics      `json:"metrics"`
}

type RepositoryReport struct {
	ID            string           `json:"id"`
	URL           string           `json:"url"`
	Commit        string           `json:"commit"`
	Language      string           `json:"language"`
	ColdIndexRuns []IndexRunReport `json:"cold_index_runs"`
	WarmIndexRuns []IndexRunReport `json:"warm_index_runs"`
	Queries       []QueryReport    `json:"queries"`
	Stats         RepositoryStats  `json:"stats"`
	Timing        TimingMetrics    `json:"timing"`
	Metrics       AggregateMetrics `json:"metrics"`
}

type Report struct {
	Tool          string             `json:"tool"`
	GeneratedAt   time.Time          `json:"generated_at"`
	SuitePath     string             `json:"suite_path"`
	SuiteRevision string             `json:"suite_revision"`
	Suite         Suite              `json:"suite"`
	Coverage      SuiteCoverage      `json:"coverage"`
	Environment   ReportEnvironment  `json:"environment"`
	Config        RunConfig          `json:"config"`
	Repositories  []RepositoryReport `json:"repositories"`
	Timing        TimingMetrics      `json:"timing"`
	Metrics       AggregateMetrics   `json:"metrics"`
}

type ReportEnvironment struct {
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	CPUInfo     string            `json:"cpu_info,omitempty"`
	NumCPU      int               `json:"num_cpu"`
	RepoRoot    string            `json:"repo_root"`
	BenchRoot   string            `json:"bench_root"`
	ToolOptions map[string]string `json:"tool_options,omitempty"`
	ToolVersion string            `json:"tool_version"`
}
