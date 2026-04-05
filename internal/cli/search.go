package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	llamaembed "github.com/uchebnick/unch/internal/embed/llama"
	"github.com/uchebnick/unch/internal/indexdb"
	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/runtime"
	appsearch "github.com/uchebnick/unch/internal/search"
	"github.com/uchebnick/unch/internal/semsearch"
	"github.com/uchebnick/unch/internal/termui"
)

func runSearch(ctx context.Context, program string, args []string, cwd string, _ indexing.FileScanner, runtimes runtime.YzmaResolver, models runtime.ModelCache) (err error) {
	defaultModelPath := defaultModelFlagValue()

	fs := flag.NewFlagSet(program+" search", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.Usage = func() {}

	root := fs.String("root", ".", "root directory used to format result paths")
	stateDir := fs.String("state-dir", "", "path to .semsearch directory; defaults to <root>/.semsearch")
	dbPath := fs.String("db", "", "deprecated: path to .semsearch/index.db, or to a .semsearch directory")
	modelPath := fs.String("model", defaultModelPath, "path to GGUF embedding model, or a known model id such as embeddinggemma or qwen3")
	libPath := fs.String("lib", "", "path to yzma library directory, or to one of its shared library files")
	queryFlag := fs.String("query", "", "search query; if empty, remaining args are joined")
	contextSize := fs.Int("ctx-size", 0, "llama context size; 0 uses the selected model default")
	batchSize := fs.Int("batch-size", 0, "llama batch size; 0 uses the selected model default")
	limit := fs.Int("limit", 10, "max number of search results")
	mode := fs.String("mode", "auto", "search mode: auto, semantic, lexical")
	maxDistance := fs.Float64("max-distance", 0.85, "maximum semantic distance kept in auto and semantic modes; <= 0 disables filtering")
	details := fs.Bool("details", false, "show detailed metadata for each search result")
	verbose := fs.Bool("verbose", false, "enable yzma verbose logging")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" search [flags] <query>",
				"Search the current index using semantic, lexical, or mixed retrieval.",
				[]string{
					cliName(program) + " search \"sqlite schema\"",
					cliName(program) + " search --mode lexical \"Run\"",
					cliName(program) + " search --details \"get path variables from a request\"",
					cliName(program) + " search --model qwen3 \"search query\"",
					cliName(program) + " search --model ~/.semsearch/models/Qwen3-Embedding-0.6B-Q8_0.gguf \"search query\"",
				},
				[]string{
					"Omit --model to reuse the default embeddinggemma GGUF model.",
					"Known model ids today: embeddinggemma and qwen3.",
					"Use the same embedding model for both index and search, otherwise ranking quality will be wrong.",
					"Switching models requires rebuilding the index with `unch index` first.",
					"Use --state-dir to search an external .semsearch directory and keep remote sync bound to that state.",
				},
			)
		}
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

	stateDirWasExplicit := false
	dbWasExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "state-dir" {
			stateDirWasExplicit = true
		}
		if f.Name == "db" {
			dbWasExplicit = true
		}
	})

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	targetPaths, resolvedIndexPath, shouldSyncRemote, err := resolveStateTarget(rootAbs, *stateDir, stateDirWasExplicit, *dbPath, dbWasExplicit)
	if err != nil {
		return err
	}

	s, err := termui.NewSession(targetPaths.LocalDir)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.Logf("fatal error: %v", err)
		}
		_ = s.Close()
	}()
	s.Logf("program=%s", program)
	s.Logf("args=%q", args)
	s.Logf("cwd=%s", cwd)
	s.Logf("command=search")

	if shouldSyncRemote {
		if _, err := semsearch.EnsureFileWeights(targetPaths.LocalDir); err != nil {
			return err
		}

		remoteSync, err := semsearch.SyncRemoteIndex(ctx, targetPaths.LocalDir)
		if err != nil {
			return fmt.Errorf("sync remote index: %w", err)
		}
		if remoteSync.Checked && remoteSync.Note != "" {
			printSessionLine(s, "%s", remoteSync.Note)
		}
	}

	resolvedLibPath, libNote, err := runtimes.ResolveOrInstallYzmaLibPath(ctx, *libPath, targetPaths.LocalDir, s)
	if err != nil {
		return err
	}
	if libNote != "" {
		s.Logf("%s", libNote)
	}

	resolvedDefaultModelPath := runtime.DefaultModelPath(targetPaths.ModelsDir)
	requestedModelPath := strings.TrimSpace(*modelPath)
	if requestedModelPath == "" {
		requestedModelPath = resolvedDefaultModelPath
	}

	resolvedModelPath, modelNote, err := models.ResolveOrInstallModelPath(ctx, requestedModelPath, resolvedDefaultModelPath, true, s)
	if err != nil {
		return err
	}
	if modelNote != "" {
		s.Logf("%s", modelNote)
	}
	modelID, err := runtime.CanonicalModelID(requestedModelPath, resolvedDefaultModelPath)
	if err != nil {
		return fmt.Errorf("resolve model id: %w", err)
	}
	resolvedContextSize := *contextSize
	if resolvedContextSize <= 0 {
		resolvedContextSize = defaultContextSize(resolvedModelPath)
	}
	resolvedBatchSize := *batchSize
	if resolvedBatchSize <= 0 {
		resolvedBatchSize = defaultBatchSize(resolvedModelPath)
	}

	s.Logf("index_db=%s", resolvedIndexPath)
	s.Logf("state_dir=%s", targetPaths.LocalDir)
	s.Logf("lib=%s", resolvedLibPath)
	s.Logf("model=%s", resolvedModelPath)
	s.Logf("model_id=%s", modelID)
	s.Logf("ctx_size=%d", resolvedContextSize)
	s.Logf("batch_size=%d", resolvedBatchSize)
	s.Logf("root=%s", rootAbs)
	s.Logf("query=%q", queryText)
	s.Logf("limit=%d", *limit)
	s.Logf("mode=%s", searchMode)
	s.Logf("max_distance=%.4f", *maxDistance)

	embedder, err := loadEmbedderWithSpinner(ctx, s, llamaembed.Config{
		ModelPath:   resolvedModelPath,
		LibPath:     resolvedLibPath,
		ContextSize: resolvedContextSize,
		BatchSize:   resolvedBatchSize,
		Verbose:     *verbose,
		Pooling:     defaultPooling(resolvedModelPath),
	})
	if err != nil {
		return err
	}
	defer embedder.Close()

	repo, err := indexdb.Open(ctx, resolvedIndexPath, embedder.Dim())
	if err != nil {
		return err
	}
	defer func() {
		_ = repo.Close()
	}()

	fileWeights, err := semsearch.LoadFileWeights(targetPaths.LocalDir)
	if err != nil {
		return err
	}

	service := appsearch.Service{
		Repo:         repo,
		Embedder:     embedder,
		PathWeighter: fileWeights,
	}

	results, err := service.Run(ctx, appsearch.Params{
		QueryText:   queryText,
		Limit:       *limit,
		Mode:        searchMode,
		MaxDistance: *maxDistance,
		ModelID:     modelID,
	}, s)
	if err != nil {
		if errors.Is(err, indexdb.ErrNoActiveSnapshot) {
			return fmt.Errorf("no active index for model %q; run `unch index --model %s` first", modelID, modelID)
		}
		return err
	}
	if len(results) == 0 {
		s.Finish("No matches found")
		return nil
	}

	s.Finish(fmt.Sprintf("Found %d matches", len(results)))
	for i, result := range results {
		var err error
		if *details {
			err = renderSearchResultDetailed(os.Stdout, i+1, rootAbs, result)
		} else {
			err = renderSearchResultCompact(os.Stdout, i+1, rootAbs, result)
		}
		if err != nil {
			return fmt.Errorf("render search result: %w", err)
		}
	}

	return nil
}
