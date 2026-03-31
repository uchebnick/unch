package cli

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	llamaembed "github.com/uchebnick/unch-searcher/internal/embed/llama"
	"github.com/uchebnick/unch-searcher/internal/indexdb"
	"github.com/uchebnick/unch-searcher/internal/indexing"
	"github.com/uchebnick/unch-searcher/internal/runtime"
	appsearch "github.com/uchebnick/unch-searcher/internal/search"
	"github.com/uchebnick/unch-searcher/internal/semsearch"
	"github.com/uchebnick/unch-searcher/internal/termui"
)

func runSearch(ctx context.Context, program string, args []string, paths semsearch.Paths, s *termui.Session, scanner indexing.FileScanner, runtimes runtime.YzmaResolver, models runtime.ModelCache) error {
	defaultDBPath := filepath.Join(paths.LocalDir, "index.db")
	defaultModelPath := filepath.Join(paths.ModelsDir, "embeddinggemma-300m.gguf")

	fs := flag.NewFlagSet(program+" search", flag.ContinueOnError)
	fs.SetOutput(nil)

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

	searchMode, err := appsearch.NormalizeMode(*mode)
	if err != nil {
		return err
	}

	modelWasExplicit := false
	dbWasExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelWasExplicit = true
		}
		if f.Name == "db" {
			dbWasExplicit = true
		}
	})

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	targetPaths, err := semsearch.PreparePaths(rootAbs)
	if err != nil {
		return err
	}

	remoteSync, err := semsearch.SyncRemoteIndex(ctx, targetPaths.LocalDir)
	if err != nil {
		return fmt.Errorf("sync remote index: %w", err)
	}
	if remoteSync.Checked && remoteSync.Note != "" {
		printSessionLine(s, "%s", remoteSync.Note)
	}

	resolvedDBPath := *dbPath
	if !dbWasExplicit {
		resolvedDBPath = filepath.Join(targetPaths.LocalDir, "index.db")
	}

	resolvedLibPath, libNote, err := runtimes.ResolveOrInstallYzmaLibPath(ctx, *libPath, targetPaths.LocalDir, s)
	if err != nil {
		return err
	}
	if libNote != "" {
		s.Logf("%s", libNote)
	}

	resolvedModelPath, modelNote, err := models.ResolveOrInstallModelPath(ctx, *modelPath, defaultModelPath, !modelWasExplicit, s)
	if err != nil {
		return err
	}
	if modelNote != "" {
		s.Logf("%s", modelNote)
	}

	s.Logf("db=%s", resolvedDBPath)
	s.Logf("lib=%s", resolvedLibPath)
	s.Logf("model=%s", resolvedModelPath)
	s.Logf("root=%s", rootAbs)
	s.Logf("query=%q", queryText)
	s.Logf("limit=%d", *limit)
	s.Logf("mode=%s", searchMode)
	s.Logf("max_distance=%.4f", *maxDistance)

	embedder, err := loadEmbedderWithSpinner(ctx, s, llamaembed.Config{
		ModelPath:   resolvedModelPath,
		LibPath:     resolvedLibPath,
		ContextSize: *contextSize,
		BatchSize:   *batchSize,
		Verbose:     *verbose,
		Pooling:     defaultPooling(),
	})
	if err != nil {
		return err
	}
	defer embedder.Close()

	repo, err := indexdb.Open(ctx, resolvedDBPath, embedder.Dim())
	if err != nil {
		return err
	}
	defer repo.Close()

	service := appsearch.Service{
		Repo:     repo,
		Embedder: embedder,
		Scanner:  scanner,
	}

	results, err := service.Run(ctx, appsearch.Params{
		QueryText:     queryText,
		CommentPrefix: *commentPrefix,
		ContextPrefix: *contextPrefix,
		Limit:         *limit,
		Mode:          searchMode,
		MaxDistance:   *maxDistance,
	}, s)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		s.Finish("No matches found")
		return nil
	}

	s.Finish(fmt.Sprintf("Found %d matches", len(results)))
	for i, result := range results {
		fmt.Printf("%2d. %s:%d  %-7s\n", i+1, formatSearchResultPath(rootAbs, result.Path), result.Line, result.DisplayMetric)
	}

	return nil
}
