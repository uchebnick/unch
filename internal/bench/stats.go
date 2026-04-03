package bench

import (
	"fmt"
	"sort"
	"strings"

	appsearch "github.com/uchebnick/unch/internal/search"
)

func BuildSuiteCoverage(suite Suite) SuiteCoverage {
	return SuiteCoverage{
		RepositoryCount: len(suite.Repositories),
		QueryCount:      SuiteQueryCount(suite),
		ModeCounts:      QueryModeCounts(suite.Repositories),
	}
}

func SuiteQueryCount(suite Suite) int {
	total := 0
	for _, repo := range suite.Repositories {
		total += len(repo.Queries)
	}
	return total
}

func QueryModeCounts(repositories []RepositoryCase) map[string]int {
	counts := make(map[string]int)
	for _, repo := range repositories {
		for _, query := range repo.Queries {
			counts[normalizeQueryMode(query.Mode)]++
		}
	}
	return counts
}

func RepositoryModeCounts(queries []QueryCase) map[string]int {
	counts := make(map[string]int)
	for _, query := range queries {
		counts[normalizeQueryMode(query.Mode)]++
	}
	return counts
}

func QueryWarmSearchMeanMS(runs []SearchRunReport) float64 {
	values := make([]float64, 0, len(runs))
	for _, run := range runs {
		values = append(values, run.DurationMS)
	}
	return meanFloat64(values)
}

func FirstTopHit(runs []SearchRunReport) *SearchHit {
	if len(runs) == 0 || len(runs[0].Hits) == 0 {
		return nil
	}
	hit := runs[0].Hits[0]
	return &hit
}

func LatestIndexRun(report RepositoryReport) (IndexRunReport, bool) {
	if len(report.WarmIndexRuns) > 0 {
		return report.WarmIndexRuns[len(report.WarmIndexRuns)-1], true
	}
	if len(report.ColdIndexRuns) > 0 {
		return report.ColdIndexRuns[len(report.ColdIndexRuns)-1], true
	}
	return IndexRunReport{}, false
}

func LatestIndexedSnapshot(report RepositoryReport) (IndexRunReport, bool) {
	for i := len(report.WarmIndexRuns) - 1; i >= 0; i-- {
		run := report.WarmIndexRuns[i]
		if run.IndexedSymbols > 0 || run.IndexedFiles > 0 {
			return run, true
		}
	}
	for i := len(report.ColdIndexRuns) - 1; i >= 0; i-- {
		run := report.ColdIndexRuns[i]
		if run.IndexedSymbols > 0 || run.IndexedFiles > 0 {
			return run, true
		}
	}
	return LatestIndexRun(report)
}

func FormatModeCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}

	preferred := []string{"auto", "semantic", "lexical"}
	parts := make([]string, 0, len(counts))
	seen := make(map[string]struct{}, len(counts))

	for _, key := range preferred {
		if value, ok := counts[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", key, value))
			seen[key] = struct{}{}
		}
	}

	var extras []string
	for key := range counts {
		if _, ok := seen[key]; ok {
			continue
		}
		extras = append(extras, key)
	}
	sort.Strings(extras)
	for _, key := range extras {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}

	return strings.Join(parts, " ")
}

func meanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func normalizeQueryMode(mode string) string {
	normalized, err := appsearch.NormalizeMode(mode)
	if err == nil && normalized != "" {
		return normalized
	}
	return strings.TrimSpace(mode)
}
