package search

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type Reporter interface {
	Logf(format string, args ...any)
}

type Repository interface {
	SearchCurrent(ctx context.Context, modelID string, queryEmbedding []float32, limit int) ([]SearchResult, error)
	ListCurrentSymbols(ctx context.Context, modelID string) ([]SearchResult, error)
}

type Embedder interface {
	EmbedQuery(text string) ([]float32, error)
}

type PathWeighter interface {
	Weight(path string) float64
}

type Result struct {
	SearchResult
	Text          string
	LexicalScore  float64
	DisplayMetric string
	sortKey       float64
}

type Params struct {
	QueryText     string
	CommentPrefix string
	ContextPrefix string
	Limit         int
	Mode          string
	MaxDistance   float64
	ModelID       string
}

type Service struct {
	Repo         Repository
	Embedder     Embedder
	PathWeighter PathWeighter
}

func NormalizeMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "auto":
		return "auto", nil
	case "semantic":
		return "semantic", nil
	case "lexical":
		return "lexical", nil
	default:
		return "", fmt.Errorf("unknown search mode %q; expected auto, semantic, or lexical", mode)
	}
}

// Run executes lexical or semantic search and returns ranked symbol matches.
func (s Service) Run(ctx context.Context, params Params, reporter Reporter) ([]Result, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}

	switch params.Mode {
	case "lexical":
		return s.searchLexicalCurrent(ctx, params, reporter)
	case "semantic":
		return s.searchSemanticCurrent(ctx, params, reporter)
	default:
		if shouldPreferLexicalSearch(params.QueryText) {
			return s.searchLexicalCurrent(ctx, params, reporter)
		}

		semanticResults, err := s.searchAutoSemanticCurrent(ctx, params, reporter)
		if err != nil {
			return nil, err
		}
		if len(semanticResults) == 0 {
			return s.searchLexicalCurrent(ctx, params, reporter)
		}

		lexicalResults, err := s.searchLexicalCurrent(ctx, params, reporter)
		if err != nil {
			return nil, err
		}
		if shouldPreferLexicalResults(params.QueryText, semanticResults, lexicalResults) {
			return lexicalResults, nil
		}
		return semanticResults, nil
	}
}

func (s Service) searchAutoSemanticCurrent(ctx context.Context, params Params, reporter Reporter) ([]Result, error) {
	results, err := s.searchSemanticCurrent(ctx, params, reporter)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 || params.MaxDistance <= 0 {
		return results, nil
	}

	// Auto mode should stay semantic-first for natural-language queries even when
	// the default distance cutoff is slightly too strict for the best candidates.
	relaxed := params
	relaxed.MaxDistance = 0
	return s.searchSemanticCurrent(ctx, relaxed, reporter)
}

func (s Service) searchSemanticCurrent(ctx context.Context, params Params, _ Reporter) ([]Result, error) {
	queryVec, err := s.Embedder.EmbedQuery(params.QueryText)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}

	candidateLimit := params.Limit * 5
	if candidateLimit < 20 {
		candidateLimit = 20
	}

	modelID := params.ModelID
	if modelID == "" {
		modelID = "embeddinggemma"
	}

	candidates, err := s.Repo.SearchCurrent(ctx, modelID, queryVec, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("search current index: %w", err)
	}

	ranked := make([]Result, 0, len(candidates))
	for _, candidate := range candidates {
		text := resultSearchText(candidate)
		fileWeight := effectiveFileWeight(s.PathWeighter, candidate.Path)

		if params.MaxDistance > 0 && candidate.Distance > params.MaxDistance {
			continue
		}

		lexicalScore := lexicalMatchScore(params.QueryText, candidate) * fileWeight
		ranked = append(ranked, Result{
			SearchResult:  candidate,
			Text:          text,
			LexicalScore:  lexicalScore,
			DisplayMetric: fmt.Sprintf("%.4f", candidate.Distance),
			sortKey:       semanticSortDistance(candidate.Distance, fileWeight),
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].sortKey != ranked[j].sortKey {
			return ranked[i].sortKey < ranked[j].sortKey
		}
		if ranked[i].Distance != ranked[j].Distance {
			return ranked[i].Distance < ranked[j].Distance
		}
		if ranked[i].LexicalScore != ranked[j].LexicalScore {
			return ranked[i].LexicalScore > ranked[j].LexicalScore
		}
		if ranked[i].Path != ranked[j].Path {
			return ranked[i].Path < ranked[j].Path
		}
		return ranked[i].Line < ranked[j].Line
	})

	if len(ranked) > params.Limit {
		ranked = ranked[:params.Limit]
	}
	return ranked, nil
}

func (s Service) searchLexicalCurrent(ctx context.Context, params Params, _ Reporter) ([]Result, error) {
	modelID := params.ModelID
	if modelID == "" {
		modelID = "embeddinggemma"
	}

	candidates, err := s.Repo.ListCurrentSymbols(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("list current symbols: %w", err)
	}

	ranked := make([]Result, 0, len(candidates))
	for _, candidate := range candidates {
		text := resultSearchText(candidate)
		score := lexicalMatchScore(params.QueryText, candidate) * effectiveFileWeight(s.PathWeighter, candidate.Path)
		if score <= 0 {
			continue
		}

		ranked = append(ranked, Result{
			SearchResult:  candidate,
			Text:          text,
			LexicalScore:  score,
			DisplayMetric: "lexical",
			sortKey:       -score,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].LexicalScore != ranked[j].LexicalScore {
			return ranked[i].LexicalScore > ranked[j].LexicalScore
		}
		if ranked[i].Path != ranked[j].Path {
			return ranked[i].Path < ranked[j].Path
		}
		return ranked[i].Line < ranked[j].Line
	})

	if len(ranked) > params.Limit {
		ranked = ranked[:params.Limit]
	}
	return ranked, nil
}

func resultSearchText(result SearchResult) string {
	var parts []string
	for _, value := range []string{
		result.Kind,
		result.Name,
		result.Container,
		result.QualifiedName,
		result.Signature,
		result.Documentation,
		result.Body,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
}

func shouldPreferLexicalResults(query string, semanticResults []Result, lexicalResults []Result) bool {
	if len(lexicalResults) == 0 {
		return false
	}
	if len(semanticResults) == 0 {
		return true
	}
	if !shouldPreferLexicalSearch(query) {
		return false
	}

	semanticTop := semanticResults[0]
	lexicalTop := lexicalResults[0]

	if semanticTop.Distance > 0.88 && lexicalTop.LexicalScore >= 0.55 {
		return true
	}
	if semanticTop.Distance > 0.82 && lexicalTop.LexicalScore >= 0.8 {
		return true
	}
	return false
}

func shouldPreferLexicalSearch(query string) bool {
	query = strings.TrimSpace(query)
	tokens := searchQueryTokens(query)
	if len(tokens) == 0 {
		return false
	}
	if !strings.ContainsFunc(query, unicode.IsSpace) && looksCodeLikeQuery(query) {
		return true
	}
	if len(tokens) == 1 && len([]rune(tokens[0])) <= 3 {
		return true
	}
	return false
}

func looksCodeLikeQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}

	hasUpper := false
	hasLower := false

	for _, r := range query {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			return true
		case strings.ContainsRune("/._-:()[]{}", r):
			return true
		}
	}

	return hasUpper && hasLower
}

func lexicalMatchScore(query string, candidate SearchResult) float64 {
	queryNorm := normalizeSearchText(query)
	if queryNorm == "" {
		return 0
	}

	textNorm := normalizeSearchText(resultSearchText(candidate))
	pathNorm := normalizeSearchText(candidate.Path)
	docNorm := strings.TrimSpace(strings.TrimSpace(textNorm + " " + pathNorm))
	if docNorm == "" {
		return 0
	}

	queryTokens := searchQueryTokens(query)
	if len(queryTokens) == 0 {
		if strings.Contains(docNorm, queryNorm) {
			return 1
		}
		return 0
	}

	score := 0.0
	if strings.Contains(textNorm, queryNorm) {
		score += 0.7
	} else if strings.Contains(docNorm, queryNorm) {
		score += 0.35
	}

	baseNorm := normalizeSearchText(filepath.Base(candidate.Path))
	textMatchedTokens := 0.0
	docMatchedTokens := 0.0
	for _, token := range queryTokens {
		textWeight, docWeight, baseWeight, pathWeight := bestLexicalWeights(token, textNorm, docNorm, baseNorm, pathNorm)
		textMatchedTokens += textWeight
		docMatchedTokens += docWeight
		if baseWeight > 0 {
			score += 0.03 * baseWeight
		} else if pathWeight > 0 {
			score += 0.01 * pathWeight
		}
	}

	score += 0.45 * float64(textMatchedTokens) / float64(len(queryTokens))
	if docMatchedTokens > textMatchedTokens {
		score += 0.1 * float64(docMatchedTokens-textMatchedTokens) / float64(len(queryTokens))
	}
	if len(queryTokens) > 1 && textMatchedTokens >= float64(len(queryTokens))*0.999 {
		score += 0.25
	} else if len(queryTokens) > 1 && docMatchedTokens >= float64(len(queryTokens))*0.999 {
		score += 0.12
	}
	if len(queryTokens) == 1 && docMatchedTokens > 0 {
		score += 0.2 * docMatchedTokens
	}

	if score > 1 {
		return 1
	}
	return score
}

func effectiveFileWeight(weighter PathWeighter, path string) float64 {
	if weighter == nil {
		return 1
	}

	weight := weighter.Weight(path)
	switch {
	case weight <= 0:
		return 1
	case weight > 1:
		return 1
	default:
		return weight
	}
}

func semanticSortDistance(distance float64, weight float64) float64 {
	if weight <= 0 || weight > 1 {
		weight = 1
	}

	similarity := 1 - distance
	if similarity <= 0 {
		return distance
	}
	return 1 - similarity*weight
}

type weightedQueryToken struct {
	token  string
	weight float64
}

func bestLexicalWeights(token string, textNorm string, docNorm string, baseNorm string, pathNorm string) (float64, float64, float64, float64) {
	var textWeight float64
	var docWeight float64
	var baseWeight float64
	var pathWeight float64

	for _, variant := range expandQueryToken(token) {
		if strings.Contains(textNorm, variant.token) && variant.weight > textWeight {
			textWeight = variant.weight
		}
		if strings.Contains(docNorm, variant.token) && variant.weight > docWeight {
			docWeight = variant.weight
		}
		if strings.Contains(baseNorm, variant.token) && variant.weight > baseWeight {
			baseWeight = variant.weight
		}
		if strings.Contains(pathNorm, variant.token) && variant.weight > pathWeight {
			pathWeight = variant.weight
		}
	}

	return textWeight, docWeight, baseWeight, pathWeight
}

func expandQueryToken(token string) []weightedQueryToken {
	add := func(items *[]weightedQueryToken, seen map[string]struct{}, value string, weight float64) {
		value = normalizeSearchText(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		*items = append(*items, weightedQueryToken{token: value, weight: weight})
	}

	seen := make(map[string]struct{})
	var expanded []weightedQueryToken
	add(&expanded, seen, token, 1.0)

	if singular := singularizeSearchToken(token); singular != token {
		add(&expanded, seen, singular, 0.92)
	}
	if plural := pluralizeSearchToken(token); plural != token {
		add(&expanded, seen, plural, 0.88)
	}

	for _, synonym := range searchTokenSynonyms(token) {
		add(&expanded, seen, synonym, 0.72)
	}

	return expanded
}

func searchTokenSynonyms(token string) []string {
	switch token {
	case "database":
		return []string{"db", "sqlite", "sql"}
	case "db":
		return []string{"database", "sqlite", "sql"}
	case "sqlite":
		return []string{"database", "db", "sql"}
	case "sql":
		return []string{"sqlite", "database", "db"}
	case "library":
		return []string{"lib", "libraries"}
	case "libraries":
		return []string{"library", "lib"}
	case "lib":
		return []string{"library", "libraries"}
	case "embedding":
		return []string{"embeddings", "vector", "vectors"}
	case "embeddings":
		return []string{"embedding", "vector", "vectors"}
	case "vector":
		return []string{"embedding", "embeddings", "vectors"}
	case "vectors":
		return []string{"vector", "embedding", "embeddings"}
	case "search":
		return []string{"query", "retrieval"}
	case "query":
		return []string{"search", "retrieval"}
	case "runtime":
		return []string{"shared", "library", "libraries"}
	case "model":
		return []string{"gguf", "embedding"}
	case "cache":
		return []string{"cached"}
	default:
		return nil
	}
}

func singularizeSearchToken(token string) string {
	switch {
	case strings.HasSuffix(token, "ies") && len(token) > 3:
		return token[:len(token)-3] + "y"
	case strings.HasSuffix(token, "es") && len(token) > 2:
		return token[:len(token)-2]
	case strings.HasSuffix(token, "s") && len(token) > 1:
		return token[:len(token)-1]
	default:
		return token
	}
}

func pluralizeSearchToken(token string) string {
	switch {
	case strings.HasSuffix(token, "y") && len(token) > 1:
		return token[:len(token)-1] + "ies"
	case strings.HasSuffix(token, "s"):
		return token
	default:
		return token + "s"
	}
}

func searchQueryTokens(query string) []string {
	normalized := normalizeSearchText(query)
	if normalized == "" {
		return nil
	}

	fields := strings.Fields(normalized)
	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		tokens = append(tokens, field)
	}
	return tokens
}

func normalizeSearchText(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}

	return strings.TrimSpace(b.String())
}
