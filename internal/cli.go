package internal

// @filectx: Command line entrypoint for indexing and semantic search over repository comments stored in sqlite.

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/hybridgroup/yzma/pkg/llama"
	_ "github.com/mattn/go-sqlite3"
	ignore "github.com/sabhiram/go-gitignore"
)

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type fileJob struct {
	Path          string
	CommentsCount int
}

type cliPaths struct {
	localDir  string
	modelsDir string
}

type rankedSearchResult struct {
	SearchResult
	Text          string
	LexicalScore  float64
	DisplayMetric string
	sortKey       float64
}

type weightedQueryToken struct {
	token  string
	weight float64
}

// @search: RunCLI is the main entrypoint and dispatches to index mode by default or to the search subcommand when the first arg is search.
func RunCLI(program string, args []string) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working dir: %w", err)
	}

	paths, err := prepareCLIPaths(cwd)
	if err != nil {
		return err
	}

	session, err := newCLISession(paths.localDir)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			session.Logf("fatal error: %v", err)
		}
		_ = session.Close()
	}()
	session.Logf("program=%s", program)
	session.Logf("args=%q", args)
	session.Logf("cwd=%s", cwd)

	command, commandArgs := detectCLICommand(args)
	session.Logf("command=%s", command)

	switch command {
	case "search":
		return runSearchCLI(ctx, program, commandArgs, paths, session)
	default:
		return runIndexCLI(ctx, program, commandArgs, paths, session)
	}
}

// @search: default local state lives in .semsearch in the current working directory and includes sqlite index db, logs, and yzma runtime libraries.
// @search: default embedding model lives in the global user cache so it is reused across different project directories.
func prepareCLIPaths(cwd string) (cliPaths, error) {
	localDir := filepath.Join(cwd, ".semsearch")
	globalDir, err := globalSemsearchDir()
	if err != nil {
		return cliPaths{}, err
	}
	modelsDir := filepath.Join(globalDir, "models")

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return cliPaths{}, fmt.Errorf("create local dir: %w", err)
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return cliPaths{}, fmt.Errorf("create global models dir: %w", err)
	}

	return cliPaths{
		localDir:  localDir,
		modelsDir: modelsDir,
	}, nil
}

func detectCLICommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "index", args
	}

	switch args[0] {
	case "index", "search":
		return args[0], args[1:]
	default:
		return "index", args
	}
}

// @search: indexing flow resolves yzma libs, resolves or downloads the GGUF model, opens sqlite, collects files, and indexes lines that start with @search:.
func runIndexCLI(ctx context.Context, program string, args []string, paths cliPaths, session *cliSession) error {
	var excludes stringListFlag

	defaultDBPath := filepath.Join(paths.localDir, "index.db")
	defaultModelPath := filepath.Join(paths.modelsDir, "embeddinggemma-300m.gguf")

	fs := flag.NewFlagSet(program+" index", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	root := fs.String("root", ".", "root directory to index")
	dbPath := fs.String("db", defaultDBPath, "path to sqlite db")
	modelPath := fs.String("model", defaultModelPath, "path to GGUF embedding model")
	libPath := fs.String("lib", "", "path to yzma library directory, or to one of its shared library files")
	contextPrefix := fs.String("context-prefix", "@filectx:", "file context prefix")
	commentPrefix := fs.String("comment-prefix", "@search:", "comment prefix")
	gitignorePath := fs.String("gitignore", "", "optional path to .gitignore; default is <root>/.gitignore")
	contextSize := fs.Int("ctx-size", 2048, "llama context size")
	batchSize := fs.Int("batch-size", 2048, "llama batch size")
	verbose := fs.Bool("verbose", false, "enable yzma verbose logging")

	fs.Var(&excludes, "exclude", "exclude pattern; can be used multiple times")

	if err := fs.Parse(args); err != nil {
		return err
	}

	modelWasExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelWasExplicit = true
		}
	})

	resolvedLibPath, libNote, err := resolveOrInstallYzmaLibPath(ctx, *libPath, paths.localDir, session)
	if err != nil {
		return err
	}
	if libNote != "" {
		session.Logf("%s", libNote)
	}

	resolvedModelPath, modelNote, err := resolveOrInstallModelPath(ctx, *modelPath, defaultModelPath, !modelWasExplicit, session)
	if err != nil {
		return err
	}
	if modelNote != "" {
		session.Logf("%s", modelNote)
	}

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	resolvedGitignore, err := resolveGitignorePath(rootAbs, *gitignorePath)
	if err != nil {
		return fmt.Errorf("resolve gitignore: %w", err)
	}

	matcher, err := buildIgnoreMatcher(resolvedGitignore, excludes)
	if err != nil {
		return fmt.Errorf("build ignore matcher: %w", err)
	}

	session.Logf("db=%s", *dbPath)
	session.Logf("lib=%s", resolvedLibPath)
	session.Logf("model=%s", resolvedModelPath)
	session.Logf("root=%s", rootAbs)

	embedder, err := loadEmbedderWithSpinner(ctx, session, EmbeddingsConfig{
		ModelPath:   resolvedModelPath,
		LibPath:     resolvedLibPath,
		ContextSize: *contextSize,
		BatchSize:   *batchSize,
		Verbose:     *verbose,
		Pooling:     llama.PoolingTypeMean,
	})
	if err != nil {
		return err
	}
	defer embedder.Close()

	db, repo, err := openRepository(ctx, *dbPath, embedder.Dim())
	if err != nil {
		return err
	}
	defer db.Close()

	indexer := NewIndexer(embedder, repo)

	jobs, totalComments, err := collectJobs(rootAbs, matcher, *commentPrefix, *contextPrefix)
	if err != nil {
		return fmt.Errorf("collect jobs: %w", err)
	}
	session.Logf("files to index=%d", len(jobs))
	session.Logf("comments to index=%d", totalComments)

	workingVersion, err := repo.WorkingVersion(ctx)
	if err != nil {
		return fmt.Errorf("get working version: %w", err)
	}
	session.Logf("working version=%d", workingVersion)

	if totalComments == 0 {
		session.Finish("No comments found")
	}

	processed := 0
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := indexer.IndexFile(ctx, job.Path, *contextPrefix, *commentPrefix, workingVersion); err != nil {
			return fmt.Errorf("index file %s: %w", job.Path, err)
		}

		processed += job.CommentsCount
		session.CountProgress("Indexing", processed, totalComments)
	}
	if totalComments > 0 {
		session.Finish(fmt.Sprintf("Indexed %d comments in %d files", totalComments, len(jobs)))
	}

	if err := repo.ActivateVersion(ctx, workingVersion); err != nil {
		return fmt.Errorf("activate version: %w", err)
	}

	if err := repo.CleanupOldVersions(ctx, workingVersion); err != nil {
		return fmt.Errorf("cleanup old versions: %w", err)
	}
	if err := repo.CleanupUnusedEmbeddings(ctx); err != nil {
		return fmt.Errorf("cleanup unused embeddings: %w", err)
	}
	session.Logf("indexing completed")
	return nil
}

// @search: search mode embeds a natural-language query, searches the current sqlite-vec index, and prints path, line, distance, and comment text.
func runSearchCLI(ctx context.Context, program string, args []string, paths cliPaths, session *cliSession) error {
	defaultDBPath := filepath.Join(paths.localDir, "index.db")
	defaultModelPath := filepath.Join(paths.modelsDir, "embeddinggemma-300m.gguf")

	fs := flag.NewFlagSet(program+" search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	root := fs.String("root", ".", "root directory used to format result paths")
	dbPath := fs.String("db", defaultDBPath, "path to sqlite db")
	modelPath := fs.String("model", defaultModelPath, "path to GGUF embedding model")
	libPath := fs.String("lib", "", "path to yzma library directory, or to one of its shared library files")
	queryFlag := fs.String("query", "", "search query; if empty, remaining args are joined")
	commentPrefix := fs.String("comment-prefix", "@search:", "comment prefix used for indexed lines")
	contextPrefix := fs.String("context-prefix", "@filectx:", "file context prefix used for indexed files")
	contextSize := fs.Int("ctx-size", 2048, "llama context size")
	batchSize := fs.Int("batch-size", 2048, "llama batch size")
	limit := fs.Int("limit", 10, "max number of search results")
	mode := fs.String("mode", "auto", "search mode: auto, semantic, lexical")
	maxDistance := fs.Float64("max-distance", 0.85, "maximum semantic distance kept in auto and semantic modes; <= 0 disables filtering")
	verbose := fs.Bool("verbose", false, "enable yzma verbose logging")

	if err := fs.Parse(args); err != nil {
		return err
	}

	queryText := strings.TrimSpace(*queryFlag)
	if queryText == "" {
		queryText = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if queryText == "" {
		return fmt.Errorf("empty search query; pass --query or provide positional text")
	}
	searchMode, err := normalizeSearchMode(*mode)
	if err != nil {
		return err
	}

	modelWasExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelWasExplicit = true
		}
	})

	resolvedLibPath, libNote, err := resolveOrInstallYzmaLibPath(ctx, *libPath, paths.localDir, session)
	if err != nil {
		return err
	}
	if libNote != "" {
		session.Logf("%s", libNote)
	}

	resolvedModelPath, modelNote, err := resolveOrInstallModelPath(ctx, *modelPath, defaultModelPath, !modelWasExplicit, session)
	if err != nil {
		return err
	}
	if modelNote != "" {
		session.Logf("%s", modelNote)
	}

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	session.Logf("db=%s", *dbPath)
	session.Logf("lib=%s", resolvedLibPath)
	session.Logf("model=%s", resolvedModelPath)
	session.Logf("root=%s", rootAbs)
	session.Logf("query=%q", queryText)
	session.Logf("limit=%d", *limit)
	session.Logf("mode=%s", searchMode)
	session.Logf("max_distance=%.4f", *maxDistance)

	embedder, err := loadEmbedderWithSpinner(ctx, session, EmbeddingsConfig{
		ModelPath:   resolvedModelPath,
		LibPath:     resolvedLibPath,
		ContextSize: *contextSize,
		BatchSize:   *batchSize,
		Verbose:     *verbose,
		Pooling:     llama.PoolingTypeMean,
	})
	if err != nil {
		return err
	}
	defer embedder.Close()

	db, repo, err := openRepository(ctx, *dbPath, embedder.Dim())
	if err != nil {
		return err
	}
	defer db.Close()

	results, err := searchCurrentResults(ctx, repo, embedder, queryText, *commentPrefix, *contextPrefix, *limit, searchMode, *maxDistance, session)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		session.Finish("No matches found")
		return nil
	}

	session.Finish(fmt.Sprintf("Found %d matches", len(results)))
	for i, result := range results {
		fmt.Printf("%2d. %s:%d  %-7s\n",
			i+1,
			formatSearchResultPath(rootAbs, result.Path),
			result.Line,
			result.DisplayMetric,
		)
	}

	return nil
}

func normalizeSearchMode(mode string) (string, error) {
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

func searchCurrentResults(
	ctx context.Context,
	repo *Repository,
	embedder *Embedder,
	queryText string,
	commentPrefix string,
	contextPrefix string,
	limit int,
	mode string,
	maxDistance float64,
	session *cliSession,
) ([]rankedSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	switch mode {
	case "lexical":
		return searchLexicalCurrent(ctx, repo, queryText, commentPrefix, contextPrefix, limit, session)
	case "semantic":
		return searchSemanticCurrent(ctx, repo, embedder, queryText, commentPrefix, contextPrefix, limit, maxDistance, session)
	default:
		if shouldPreferLexicalSearch(queryText) {
			return searchLexicalCurrent(ctx, repo, queryText, commentPrefix, contextPrefix, limit, session)
		}

		semanticResults, err := searchSemanticCurrent(ctx, repo, embedder, queryText, commentPrefix, contextPrefix, limit, maxDistance, session)
		if err != nil {
			return nil, err
		}
		if len(semanticResults) == 0 {
			return searchLexicalCurrent(ctx, repo, queryText, commentPrefix, contextPrefix, limit, session)
		}

		lexicalResults, err := searchLexicalCurrent(ctx, repo, queryText, commentPrefix, contextPrefix, limit, session)
		if err != nil {
			return nil, err
		}
		if shouldPreferLexicalResults(semanticResults, lexicalResults) {
			return lexicalResults, nil
		}
		return semanticResults, nil
	}
}

func searchSemanticCurrent(
	ctx context.Context,
	repo *Repository,
	embedder *Embedder,
	queryText string,
	commentPrefix string,
	contextPrefix string,
	limit int,
	maxDistance float64,
	session *cliSession,
) ([]rankedSearchResult, error) {
	queryVec, err := embedder.EmbedQuery(queryText)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}

	candidateLimit := limit * 5
	if candidateLimit < 20 {
		candidateLimit = 20
	}

	candidates, err := repo.SearchCurrent(ctx, queryVec, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("search current index: %w", err)
	}

	ranked := make([]rankedSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		text, _, err := readSearchResultContent(candidate.Path, candidate.Line, commentPrefix, contextPrefix)
		if err != nil && session != nil {
			session.Logf("read result snippet %s:%d: %v", candidate.Path, candidate.Line, err)
		}

		if maxDistance > 0 && candidate.Distance > maxDistance {
			continue
		}

		ranked = append(ranked, rankedSearchResult{
			SearchResult:  candidate,
			Text:          text,
			LexicalScore:  lexicalMatchScore(queryText, candidate.Path, text),
			DisplayMetric: fmt.Sprintf("%.4f", candidate.Distance),
			sortKey:       candidate.Distance,
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

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func searchLexicalCurrent(
	ctx context.Context,
	repo *Repository,
	queryText string,
	commentPrefix string,
	contextPrefix string,
	limit int,
	session *cliSession,
) ([]rankedSearchResult, error) {
	candidates, err := repo.ListCurrentComments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current comments: %w", err)
	}

	ranked := make([]rankedSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		text, _, err := readSearchResultContent(candidate.Path, candidate.Line, commentPrefix, contextPrefix)
		if err != nil && session != nil {
			session.Logf("read lexical result snippet %s:%d: %v", candidate.Path, candidate.Line, err)
		}

		score := lexicalMatchScore(queryText, candidate.Path, text)
		if score <= 0 {
			continue
		}

		ranked = append(ranked, rankedSearchResult{
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

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func shouldPreferLexicalResults(semanticResults []rankedSearchResult, lexicalResults []rankedSearchResult) bool {
	if len(lexicalResults) == 0 {
		return false
	}
	if len(semanticResults) == 0 {
		return true
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
	tokens := searchQueryTokens(query)
	if len(tokens) == 0 {
		return false
	}
	if looksCodeLikeQuery(query) {
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

func lexicalMatchScore(query string, path string, text string) float64 {
	queryNorm := normalizeSearchText(query)
	if queryNorm == "" {
		return 0
	}

	textNorm := normalizeSearchText(text)
	pathNorm := normalizeSearchText(path)
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

	baseNorm := normalizeSearchText(filepath.Base(path))
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

func openRepository(ctx context.Context, dbPath string, dim int) (*sql.DB, *Repository, error) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("ping db: %w", err)
	}

	repo := NewRepository(db, dim)
	if err := repo.Init(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("init repository: %w", err)
	}

	return db, repo, nil
}

func formatSearchResultPath(root string, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return target
	}
	if rel == "." {
		return rel
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return target
	}
	return filepath.ToSlash(rel)
}

func readSearchResultContent(path string, line int, commentPrefix string, contextPrefix string) (string, string, error) {
	comments, context, err := ExtractPrefixedBlocks(path, commentPrefix, contextPrefix)
	if err == nil {
		for _, comment := range comments {
			if comment.Line == line {
				return comment.Text, context, nil
			}
		}
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if err != nil {
			return "", "", err
		}
		return "", "", readErr
	}

	lines := strings.Split(normalizeCommentText(string(data)), "\n")
	if line <= 0 || line > len(lines) {
		if err != nil {
			return "", "", err
		}
		return "", strings.TrimSpace(context), nil
	}

	text := lines[line-1]
	if payload, ok := extractDirectivePayload(text, commentPrefix); ok {
		text = payload
	} else {
		text = strings.TrimSpace(text)
	}

	if err != nil {
		return text, "", err
	}
	return text, strings.TrimSpace(context), nil
}

func loadEmbedderWithSpinner(ctx context.Context, session *cliSession, cfg EmbeddingsConfig) (*Embedder, error) {
	done := make(chan struct{})

	go func() {
		frames := []rune{'|', '/', '-', '\\'}
		idx := 0
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			default:
				session.ui.Status(fmt.Sprintf("Loading model      %c", frames[idx%len(frames)]))
				time.Sleep(120 * time.Millisecond)
				idx++
			}
		}
	}()

	embedder, err := NewEmbedder(cfg)
	close(done)

	if err != nil {
		session.ui.Clear()
		return nil, fmt.Errorf("load embedder: %w", err)
	}

	session.Finish(fmt.Sprintf("Loaded model       dim=%d", embedder.Dim()))
	return embedder, nil
}

func collectJobs(root string, matcher *ignore.GitIgnore, commentPrefix string, contextPrefix string) ([]fileJob, int, error) {
	var jobs []fileJob
	totalComments := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}

		if shouldSkipIndexedPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if matcher != nil && matcher.MatchesPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		comments, _, err := ExtractPrefixedBlocks(path, commentPrefix, contextPrefix)
		if err != nil {
			return fmt.Errorf("extract comments from %s: %w", path, err)
		}

		if len(comments) == 0 {
			return nil
		}

		jobs = append(jobs, fileJob{
			Path:          path,
			CommentsCount: len(comments),
		})
		totalComments += len(comments)

		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	return jobs, totalComments, nil
}
