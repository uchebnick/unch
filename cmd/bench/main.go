package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/uchebnick/unch/internal/bench"
)

type stringMapFlag map[string]string

func (s *stringMapFlag) String() string {
	if s == nil || len(*s) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", map[string]string(*s))
}

func (s *stringMapFlag) Set(value string) error {
	key, rawValue, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("expected key=value, got %q", value)
	}
	if *s == nil {
		*s = make(map[string]string)
	}
	(*s)[strings.TrimSpace(key)] = strings.TrimSpace(rawValue)
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		fatalf("get working dir: %v", err)
	}

	defaultBenchRoot := envOrDefault("UNCH_BENCH_ROOT", filepath.Join(os.TempDir(), "unch-bench"))
	defaultSuitePath := envOrDefault("UNCH_BENCH_SUITE", filepath.Join(cwd, "benchmarks", "suites", "default.json"))
	defaultResultsDir := filepath.Join(cwd, "benchmarks", "results")

	var toolOptions stringMapFlag

	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	toolName := fs.String("tool", "unch", "tool adapter to benchmark")
	suitePath := fs.String("suite", defaultSuitePath, "path to benchmark suite json")
	benchRoot := fs.String("bench-root", defaultBenchRoot, "benchmark workspace root")
	resultsDir := fs.String("results-dir", defaultResultsDir, "directory where benchmark json reports are written")
	outputPath := fs.String("output", "", "optional explicit json output path")
	coldIndexRuns := fs.Int("cold-index-runs", 1, "number of cold indexing runs per repository")
	warmIndexRuns := fs.Int("warm-index-runs", 3, "number of warm indexing runs per repository")
	warmSearchRuns := fs.Int("warm-search-runs", 5, "number of warm search runs per query")
	searchLimit := fs.Int("search-limit", 10, "number of ranked hits requested from the tool during search benchmarking")
	fs.Var(&toolOptions, "tool-option", "tool-specific option in key=value form; can be repeated")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fatalf("parse flags: %v", err)
	}

	suite, err := bench.LoadSuite(*suitePath)
	if err != nil {
		fatalf("load suite: %v", err)
	}

	env, err := bench.NewEnvironment(cwd, *suitePath, *benchRoot, *resultsDir, toolOptions)
	if err != nil {
		fatalf("prepare environment: %v", err)
	}

	adapter, err := newAdapter(*toolName)
	if err != nil {
		fatalf("%v", err)
	}

	report, err := bench.RunBenchmark(ctx, adapter, suite, env, bench.RunConfig{
		ColdIndexRuns:  *coldIndexRuns,
		WarmIndexRuns:  *warmIndexRuns,
		WarmSearchRuns: *warmSearchRuns,
		SearchLimit:    *searchLimit,
	}, os.Stdout)
	if err != nil {
		fatalf("run benchmark: %v", err)
	}

	if err := bench.PrintSummary(os.Stdout, report); err != nil {
		fatalf("print summary: %v", err)
	}

	path := *outputPath
	if path == "" {
		filename := fmt.Sprintf("%s-%s.json", report.Tool, time.Now().UTC().Format("20060102T150405Z"))
		path = filepath.Join(env.ResultsDir, filename)
	}
	if err := bench.WriteReportJSON(path, report); err != nil {
		fatalf("write report: %v", err)
	}
	fmt.Printf("\nWrote report: %s\n", path)
}

func newAdapter(toolName string) (bench.Adapter, error) {
	switch toolName {
	case "unch":
		return &bench.UnchAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", toolName)
	}
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
