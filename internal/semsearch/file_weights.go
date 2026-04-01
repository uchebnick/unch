package semsearch

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// DefaultFileWeightsJSON defines the baseline ranking penalties applied to common
// low-signal file groups. When multiple patterns match, the smallest weight wins.
const DefaultFileWeightsJSON = `{
  "**/*_test.*": 0.82,
  "**/*.test.*": 0.84,
  "**/*.spec.*": 0.84,
  "**/test_*.py": 0.84,
  "**/test/**": 0.9,
  "**/tests/**": 0.9,
  "**/testdata/**": 0.78,
  "**/spec/**": 0.9,
  "**/specs/**": 0.9,
  "**/example*.*": 0.88,
  "**/example*/**": 0.88,
  "**/examples/**": 0.88,
  "**/sample*/**": 0.9,
  "**/samples/**": 0.9,
  "**/demo/**": 0.9,
  "**/demos/**": 0.9,
  "**/benchmark*/**": 0.9,
  "**/benchmarks/**": 0.9,
  "**/*_bench.*": 0.9,
  "**/fixture*/**": 0.84,
  "**/fixtures/**": 0.84,
  "**/__fixtures__/**": 0.84,
  "**/mock*/**": 0.9,
  "**/mocks/**": 0.9,
  "**/__mocks__/**": 0.9,
  "**/stubs/**": 0.9,
  "**/vendor/**": 0.72,
  "**/third_party/**": 0.78,
  "**/dist/**": 0.7,
  "**/build/**": 0.72,
  "**/node_modules/**": 0.35
}
`

type compiledFileWeightRule struct {
	pattern string
	weight  float64
	matcher *ignore.GitIgnore
}

// FileWeights stores compiled path weighting rules loaded from .semsearch/file_weights.json.
type FileWeights struct {
	rules []compiledFileWeightRule
}

// FileWeightsPath returns the local ranking config file path inside .semsearch.
func FileWeightsPath(localDir string) string {
	return filepath.Join(localDir, "file_weights.json")
}

// EnsureFileWeights creates the baseline ranking config when it is missing.
func EnsureFileWeights(localDir string) (bool, error) {
	path := FileWeightsPath(localDir)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(DefaultFileWeightsJSON), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// LoadFileWeights parses the local ranking config. Missing files resolve to an empty rule set.
func LoadFileWeights(localDir string) (*FileWeights, error) {
	path := FileWeightsPath(localDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &FileWeights{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	patterns := make([]string, 0, len(raw))
	for pattern := range raw {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	rules := make([]compiledFileWeightRule, 0, len(patterns))
	for _, pattern := range patterns {
		weight := raw[pattern]
		if err := validateFileWeight(pattern, weight); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", path, err)
		}
		rules = append(rules, compiledFileWeightRule{
			pattern: pattern,
			weight:  weight,
			matcher: ignore.CompileIgnoreLines(pattern),
		})
	}

	return &FileWeights{rules: rules}, nil
}

// Weight returns the minimum configured weight for the given repository-relative path.
func (fw *FileWeights) Weight(path string) float64 {
	if fw == nil || len(fw.rules) == 0 {
		return 1
	}

	normalized := filepath.ToSlash(strings.TrimSpace(path))
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" {
		return 1
	}

	weight := 1.0
	for _, rule := range fw.rules {
		if rule.matcher.MatchesPath(normalized) && rule.weight < weight {
			weight = rule.weight
		}
	}
	return weight
}

func validateFileWeight(pattern string, weight float64) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("empty glob pattern")
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) {
		return fmt.Errorf("weight for %q must be finite", pattern)
	}
	if weight <= 0 || weight > 1 {
		return fmt.Errorf("weight for %q must be in (0, 1]", pattern)
	}
	return nil
}
