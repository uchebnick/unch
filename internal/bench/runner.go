package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func RunBenchmark(ctx context.Context, adapter Adapter, suite Suite, env Environment, cfg RunConfig, progress io.Writer) (Report, error) {
	cfg = normalizeRunConfig(cfg)
	tracker := newProgressTracker(progress, suite, cfg)

	tracker.Printf("Preparing adapter %s", adapter.Name())
	if err := adapter.Prepare(ctx, env); err != nil {
		return Report{}, fmt.Errorf("prepare adapter %s: %w", adapter.Name(), err)
	}

	suiteRevision, err := SuiteRevision(env.SuitePath)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		Tool:          adapter.Name(),
		GeneratedAt:   time.Now().UTC(),
		SuitePath:     env.SuitePath,
		SuiteRevision: suiteRevision,
		Suite:         suite,
		Coverage:      BuildSuiteCoverage(suite),
		Environment: ReportEnvironment{
			OS:          env.OS,
			Arch:        env.Arch,
			CPUInfo:     env.CPUInfo,
			NumCPU:      env.NumCPU,
			RepoRoot:    env.RepoRoot,
			BenchRoot:   env.BenchRoot,
			ToolOptions: cloneMap(env.ToolOptions),
			ToolVersion: adapter.Version(),
		},
		Config: cfg,
	}

	var allColdDurations []time.Duration
	var allWarmIndexDurations []time.Duration
	var allWarmSearchDurations []time.Duration
	var allQueryMetrics []QueryMetrics

	for repoIndex, repoCase := range suite.Repositories {
		tracker.Printf("Checkout [%d/%d] %s@%s", repoIndex+1, len(suite.Repositories), repoCase.ID, shortCommit(repoCase.Commit))
		checkedOutRepo, err := ensureRepoCheckout(ctx, repoCase, env.ReposRoot)
		if err != nil {
			return Report{}, err
		}

		repoReport, coldDurations, warmIndexDurations, warmSearchDurations, queryMetrics, err := benchmarkRepository(ctx, adapter, checkedOutRepo, env, cfg, tracker)
		if err != nil {
			return Report{}, err
		}

		report.Repositories = append(report.Repositories, repoReport)
		allColdDurations = append(allColdDurations, coldDurations...)
		allWarmIndexDurations = append(allWarmIndexDurations, warmIndexDurations...)
		allWarmSearchDurations = append(allWarmSearchDurations, warmSearchDurations...)
		allQueryMetrics = append(allQueryMetrics, queryMetrics...)
	}

	report.Timing = TimingMetrics{
		ColdIndexMeanMS:  durationMS(meanDuration(allColdDurations)),
		WarmIndexMeanMS:  durationMS(meanDuration(allWarmIndexDurations)),
		WarmSearchMeanMS: durationMS(meanDuration(allWarmSearchDurations)),
	}
	report.Metrics = AggregateQueryMetrics(allQueryMetrics)

	return report, nil
}

func benchmarkRepository(ctx context.Context, adapter Adapter, repo CheckedOutRepo, env Environment, cfg RunConfig, tracker *progressTracker) (RepositoryReport, []time.Duration, []time.Duration, []time.Duration, []QueryMetrics, error) {
	report := RepositoryReport{
		ID:       repo.Case.ID,
		URL:      repo.Case.URL,
		Commit:   repo.Case.Commit,
		Language: repo.Case.Language,
	}

	var coldDurations []time.Duration
	for i := 0; i < cfg.ColdIndexRuns; i++ {
		tracker.Step(repo.Case.ID, fmt.Sprintf("cold index %d/%d", i+1, cfg.ColdIndexRuns))
		if err := removeLocalIndexState(repo.Root); err != nil {
			return RepositoryReport{}, nil, nil, nil, nil, fmt.Errorf("remove local index state for %s cold run: %w", repo.Case.ID, err)
		}
		result, err := adapter.Index(ctx, repo, env, cfg)
		if err != nil {
			return RepositoryReport{}, nil, nil, nil, nil, err
		}
		report.ColdIndexRuns = append(report.ColdIndexRuns, indexRunToReport(result))
		coldDurations = append(coldDurations, result.Duration)
	}

	var warmIndexDurations []time.Duration
	if cfg.WarmIndexRuns > 0 && !hasLocalIndexState(repo.Root) {
		tracker.Printf("Preparing warm baseline for %s", repo.Case.ID)
		if err := removeLocalIndexState(repo.Root); err != nil {
			return RepositoryReport{}, nil, nil, nil, nil, fmt.Errorf("remove local index state for %s warm baseline: %w", repo.Case.ID, err)
		}
		if _, err := adapter.Index(ctx, repo, env, cfg); err != nil {
			return RepositoryReport{}, nil, nil, nil, nil, err
		}
	}
	for i := 0; i < cfg.WarmIndexRuns; i++ {
		tracker.Step(repo.Case.ID, fmt.Sprintf("warm index %d/%d", i+1, cfg.WarmIndexRuns))
		result, err := adapter.Index(ctx, repo, env, cfg)
		if err != nil {
			return RepositoryReport{}, nil, nil, nil, nil, err
		}
		report.WarmIndexRuns = append(report.WarmIndexRuns, indexRunToReport(result))
		warmIndexDurations = append(warmIndexDurations, result.Duration)
	}

	if len(report.WarmIndexRuns) == 0 && len(report.ColdIndexRuns) == 0 {
		return RepositoryReport{}, nil, nil, nil, nil, fmt.Errorf("repository %s has no completed index runs", repo.Case.ID)
	}

	var allWarmSearchDurations []time.Duration
	var queryMetrics []QueryMetrics

	for _, query := range repo.Case.Queries {
		queryReport := QueryReport{
			ID:           query.ID,
			Text:         query.Text,
			Mode:         query.Mode,
			ExpectedHits: append([]string(nil), query.ExpectedHits...),
		}

		var scoredHits []SearchHit
		var querySearchDurations []time.Duration
		for i := 0; i < cfg.WarmSearchRuns; i++ {
			tracker.Step(repo.Case.ID, fmt.Sprintf("search %s %d/%d", query.ID, i+1, cfg.WarmSearchRuns))
			result, err := adapter.Search(ctx, repo, query, env, cfg)
			if err != nil {
				return RepositoryReport{}, nil, nil, nil, nil, err
			}
			if i == 0 {
				scoredHits = append(scoredHits, result.Hits...)
			}
			querySearchDurations = append(querySearchDurations, result.Duration)
			queryReport.Runs = append(queryReport.Runs, searchRunToReport(result))
			allWarmSearchDurations = append(allWarmSearchDurations, result.Duration)
		}

		queryReport.Timing = QueryTiming{
			WarmSearchMeanMS: durationMS(meanDuration(querySearchDurations)),
		}
		queryReport.TopHit = FirstTopHit(queryReport.Runs)
		queryReport.Metrics = ScoreQuery(query.ExpectedHits, scoredHits)
		queryMetrics = append(queryMetrics, queryReport.Metrics)
		report.Queries = append(report.Queries, queryReport)
	}

	report.Stats = RepositoryStats{
		QueryCount: len(repo.Case.Queries),
		ModeCounts: RepositoryModeCounts(repo.Case.Queries),
	}
	if latestIndexRun, ok := LatestIndexedSnapshot(report); ok {
		report.Stats.LastIndexedSymbols = latestIndexRun.IndexedSymbols
		report.Stats.LastIndexedFiles = latestIndexRun.IndexedFiles
	}

	report.Timing = TimingMetrics{
		ColdIndexMeanMS:  durationMS(meanDuration(coldDurations)),
		WarmIndexMeanMS:  durationMS(meanDuration(warmIndexDurations)),
		WarmSearchMeanMS: durationMS(meanDuration(allWarmSearchDurations)),
	}
	report.Metrics = AggregateQueryMetrics(queryMetrics)

	return report, coldDurations, warmIndexDurations, allWarmSearchDurations, queryMetrics, nil
}

type progressTracker struct {
	out     io.Writer
	total   int
	current int
}

func newProgressTracker(out io.Writer, suite Suite, cfg RunConfig) *progressTracker {
	if out == nil {
		return &progressTracker{}
	}
	total := 1
	for _, repo := range suite.Repositories {
		total += cfg.ColdIndexRuns
		total += cfg.WarmIndexRuns
		total += len(repo.Queries) * cfg.WarmSearchRuns
	}
	return &progressTracker{out: out, total: total}
}

func (p *progressTracker) Printf(format string, args ...any) {
	if p == nil || p.out == nil {
		return
	}
	_, _ = fmt.Fprintf(p.out, "[bench] %s\n", fmt.Sprintf(format, args...))
}

func (p *progressTracker) Step(repo string, detail string) {
	if p == nil || p.out == nil {
		return
	}
	p.current++
	_, _ = fmt.Fprintf(p.out, "[bench] %s %3d/%-3d %s %s\n", progressBar(p.current, p.total, 20), p.current, p.total, repo, detail)
}

func progressBar(current int, total int, width int) string {
	if total <= 0 {
		total = 1
	}
	if width <= 0 {
		width = 10
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	filled := int(float64(current) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) <= 7 {
		return commit
	}
	return commit[:7]
}

func ensureRepoCheckout(ctx context.Context, repoCase RepositoryCase, reposRoot string) (CheckedOutRepo, error) {
	repoRoot := filepath.Join(reposRoot, filepath.FromSlash(repoCase.ID))
	gitDir := filepath.Join(repoRoot, ".git")

	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(repoRoot), 0o755); err != nil {
			return CheckedOutRepo{}, fmt.Errorf("create repo checkout dir for %s: %w", repoCase.ID, err)
		}
		if _, err := runGit(ctx, "", "clone", "--no-checkout", repoCase.URL, repoRoot); err != nil {
			return CheckedOutRepo{}, fmt.Errorf("clone %s: %w", repoCase.ID, err)
		}
	} else if err != nil {
		return CheckedOutRepo{}, fmt.Errorf("stat checkout for %s: %w", repoCase.ID, err)
	}

	if _, err := runGit(ctx, repoRoot, "fetch", "--depth", "1", "origin", repoCase.Commit); err != nil {
		return CheckedOutRepo{}, fmt.Errorf("fetch %s@%s: %w", repoCase.ID, repoCase.Commit, err)
	}
	if _, err := runGit(ctx, repoRoot, "checkout", "--detach", "--force", repoCase.Commit); err != nil {
		return CheckedOutRepo{}, fmt.Errorf("checkout %s@%s: %w", repoCase.ID, repoCase.Commit, err)
	}
	if _, err := runGit(ctx, repoRoot, "clean", "-fdx"); err != nil {
		return CheckedOutRepo{}, fmt.Errorf("clean %s checkout: %w", repoCase.ID, err)
	}

	return CheckedOutRepo{Case: repoCase, Root: repoRoot}, nil
}

func removeLocalIndexState(repoRoot string) error {
	return os.RemoveAll(filepath.Join(repoRoot, ".semsearch"))
}

func hasLocalIndexState(repoRoot string) bool {
	_, err := os.Stat(filepath.Join(repoRoot, ".semsearch", "index.db"))
	return err == nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func normalizeRunConfig(cfg RunConfig) RunConfig {
	if cfg.ColdIndexRuns <= 0 {
		cfg.ColdIndexRuns = 1
	}
	if cfg.WarmIndexRuns <= 0 {
		cfg.WarmIndexRuns = 3
	}
	if cfg.WarmSearchRuns <= 0 {
		cfg.WarmSearchRuns = 5
	}
	if cfg.SearchLimit <= 0 {
		cfg.SearchLimit = 10
	}
	return cfg
}

func indexRunToReport(result IndexRunResult) IndexRunReport {
	return IndexRunReport{
		Summary:        result.Summary,
		IndexedSymbols: result.IndexedSymbols,
		IndexedFiles:   result.IndexedFiles,
		DurationMS:     durationMS(result.Duration),
	}
}

func searchRunToReport(result SearchRunResult) SearchRunReport {
	return SearchRunReport{
		DurationMS: durationMS(result.Duration),
		Hits:       append([]SearchHit(nil), result.Hits...),
	}
}

func WriteReportJSON(path string, report Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func PrintSummary(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Tool: %s (%s)\n", report.Tool, report.Environment.ToolVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Suite: %s [%s v%d]\n", report.SuitePath, report.Suite.ID, report.Suite.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Suite revision: %s\n", report.SuiteRevision); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Environment: %s/%s", report.Environment.OS, report.Environment.Arch); err != nil {
		return err
	}
	if report.Environment.CPUInfo != "" {
		if _, err := fmt.Fprintf(w, " • %s", report.Environment.CPUInfo); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, " • %d cores\n", report.Environment.NumCPU); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Suite coverage: %d repos • %d queries • %s\n", report.Coverage.RepositoryCount, report.Coverage.QueryCount, FormatModeCounts(report.Coverage.ModeCounts)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Run profile: %d cold / %d warm / %d search repeats • top %d hits\n", report.Config.ColdIndexRuns, report.Config.WarmIndexRuns, report.Config.WarmSearchRuns, report.Config.SearchLimit); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cold index mean: %s\n", formatDurationMS(report.Timing.ColdIndexMeanMS)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Warm index mean: %s\n", formatDurationMS(report.Timing.WarmIndexMeanMS)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Warm search mean: %s\n", formatDurationMS(report.Timing.WarmSearchMeanMS)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Quality: %d/100 (top1=%.3f top3=%.3f mrr=%.3f)\n", report.Metrics.QualityScore, report.Metrics.Top1, report.Metrics.Top3, report.Metrics.MRR); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	for _, repo := range report.Repositories {
		if _, err := fmt.Fprintf(w, "- %s [%s] • %d queries • %s\n", repo.ID, repo.Language, repo.Stats.QueryCount, FormatModeCounts(repo.Stats.ModeCounts)); err != nil {
			return err
		}
		if repo.Stats.LastIndexedSymbols > 0 || repo.Stats.LastIndexedFiles > 0 {
			if _, err := fmt.Fprintf(w, "  latest index snapshot: %d symbols / %d files\n", repo.Stats.LastIndexedSymbols, repo.Stats.LastIndexedFiles); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  pinned commit: %s\n", shortCommit(repo.Commit)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  cold index: %s\n", formatDurationMS(repo.Timing.ColdIndexMeanMS)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  warm index: %s\n", formatDurationMS(repo.Timing.WarmIndexMeanMS)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  warm search: %s\n", formatDurationMS(repo.Timing.WarmSearchMeanMS)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  quality: %d/100 (top1=%.3f top3=%.3f mrr=%.3f)\n", repo.Metrics.QualityScore, repo.Metrics.Top1, repo.Metrics.Top3, repo.Metrics.MRR); err != nil {
			return err
		}
	}

	misses := collectRankingMisses(report)
	if len(misses) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Top1 misses: %d\n", len(misses)); err != nil {
			return err
		}
		maxMisses := 10
		for i, miss := range misses {
			if i >= maxMisses {
				if _, err := fmt.Fprintf(w, "... %d more misses omitted\n", len(misses)-maxMisses); err != nil {
					return err
				}
				break
			}
			if _, err := fmt.Fprintf(w, "- %s / %s (%s, %s): expected %s, got %s\n",
				miss.repoID,
				miss.query.ID,
				normalizeQueryMode(miss.query.Mode),
				formatDurationMS(miss.query.Timing.WarmSearchMeanMS),
				formatExpectedHits(miss.query.ExpectedHits),
				formatTopHitWithRank(miss.query),
			); err != nil {
				return err
			}
		}
	}

	return nil
}

type rankingMiss struct {
	repoID string
	query  QueryReport
}

func collectRankingMisses(report Report) []rankingMiss {
	var misses []rankingMiss
	for _, repo := range report.Repositories {
		for _, query := range repo.Queries {
			if query.Metrics.Top1Success {
				continue
			}
			misses = append(misses, rankingMiss{repoID: repo.ID, query: query})
		}
	}
	return misses
}

func formatExpectedHits(hits []string) string {
	return strings.Join(hits, ", ")
}

func formatTopHitWithRank(query QueryReport) string {
	if query.TopHit == nil {
		return "no hits"
	}
	if query.Metrics.ObservedRank > 0 {
		return fmt.Sprintf("%s:%d @ rank %d", query.TopHit.Path, query.TopHit.Line, query.Metrics.ObservedRank)
	}
	return fmt.Sprintf("%s:%d", query.TopHit.Path, query.TopHit.Line)
}

func formatDurationMS(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.2fs", ms/1000)
	}
	return fmt.Sprintf("%.2fms", ms)
}
