package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/filehashdb"
	"github.com/uchebnick/unch/internal/indexdb"
	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/runtime"
	"github.com/uchebnick/unch/internal/semsearch"
	"github.com/uchebnick/unch/internal/termui"
)

func runIndex(ctx context.Context, program string, args []string, cwd string, scanner indexing.FileScanner, runtimes runtime.YzmaResolver, models runtime.ModelCache) (err error) {
	var excludes stringListFlag

	defaultModelPath := defaultModelFlagValue()

	fs := flag.NewFlagSet(program+" index", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.Usage = func() {}

	root := fs.String("root", ".", "root directory to index")
	stateDir := fs.String("state-dir", "", "path to .semsearch directory; defaults to <root>/.semsearch")
	dbPath := fs.String("db", "", "deprecated: path to .semsearch/index.db, or to a .semsearch directory")
	modelPath := fs.String("model", defaultModelPath, "path to GGUF embedding model, or a known model id such as embeddinggemma or qwen3")
	provider := fs.String("provider", appembed.DefaultProvider().String(), "embedding provider: llama.cpp or openrouter")
	libPath := fs.String("lib", "", "path to yzma library directory, or to one of its shared library files")
	contextPrefix := fs.String("context-prefix", "@filectx:", "legacy file context prefix used only by fallback indexers")
	commentPrefix := fs.String("comment-prefix", "@search:", "legacy comment prefix used only by fallback indexers")
	gitignorePath := fs.String("gitignore", "", "optional path to .gitignore; default is <root>/.gitignore")
	contextSize := fs.Int("ctx-size", 0, "llama context size; 0 uses the selected model default")
	verbose := fs.Bool("verbose", false, "enable yzma verbose logging")
	fs.Var(&excludes, "exclude", "exclude pattern; can be used multiple times")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printFlagSetHelp(
				os.Stdout,
				fs,
				cliName(program)+" index [flags]",
				"Build or refresh the local search index for a repository.",
				[]string{
					cliName(program) + " index --root .",
					cliName(program) + " index --exclude node_modules --exclude dist",
					cliName(program) + " index --model qwen3",
					cliName(program) + " index --provider openrouter --model openai/text-embedding-3-small",
					cliName(program) + " index --model ~/.semsearch/models/Qwen3-Embedding-0.6B-Q8_0.gguf",
				},
				[]string{
					"Omit --model to auto-download the default embeddinggemma GGUF model.",
					"Use --provider openrouter with --model <remote-model-id>; token lookup checks OPENROUTER_API_KEY, then the global unch tokens.json, then .semsearch/tokens.json.",
					"Known model ids today: embeddinggemma and qwen3.",
					"Changing --model changes the embedding space; rebuild the index before searching with the new model.",
					"--comment-prefix and --context-prefix are legacy fallback knobs for unsupported files or parser failures.",
					"Use --state-dir to keep index artifacts in a custom .semsearch directory.",
				},
			)
		}
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

	targetPaths, resolvedIndexPath, _, err := resolveStateTarget(rootAbs, *stateDir, stateDirWasExplicit, *dbPath, dbWasExplicit)
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
	s.Logf("command=index")

	if _, err := semsearch.EnsureFileWeights(targetPaths.LocalDir); err != nil {
		return err
	}
	scanner.Root = rootAbs

	currentManifest, err := semsearch.ReadManifest(targetPaths.LocalDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read manifest: %w", err)
	}
	if err == nil && semsearch.HasRemoteBinding(currentManifest) {
		printSessionLine(s, "Warning: local indexing will detach this repository from remote CI updates and switch the manifest back to local")
		confirmed, confirmErr := confirmRemoteReindex(s, os.Stdin, os.Stderr, s != nil && s.Interactive() && isCharDevice(os.Stdin))
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return fmt.Errorf("local reindex canceled; remote CI binding preserved")
		}
	}

	resolvedGitignore, err := indexing.ResolveGitignorePath(rootAbs, *gitignorePath)
	if err != nil {
		return fmt.Errorf("resolve gitignore: %w", err)
	}

	hashStore, err := filehashdb.Open(ctx, targetPaths.FileHashDB)
	if err != nil {
		return fmt.Errorf("open file hash db: %w", err)
	}
	defer func() {
		_ = hashStore.Close()
	}()

	scannerFingerprint := indexing.BuildScannerFingerprint(*commentPrefix, *contextPrefix, excludes)

	s.Logf("scanner_fingerprint=%s", scannerFingerprint)

	prepared, err := prepareEmbedder(
		ctx,
		s,
		targetPaths,
		*provider,
		*modelPath,
		*libPath,
		*contextSize,
		*verbose,
		runtimes,
		models,
	)
	if err != nil {
		return err
	}
	defer prepared.Embedder.Close()

	var currentFileHashes map[string]string
	if currentState, ok, err := hashStore.Current(ctx, prepared.Provider.String(), prepared.ModelID); err != nil {
		return fmt.Errorf("read current file hash state: %w", err)
	} else if ok && currentState.ScannerFingerprint == scannerFingerprint {
		currentFileHashes = currentState.Files
	}
	s.Logf("current_file_hashes=%d", len(currentFileHashes))

	fileHashStateVersion, err := hashStore.BeginState(ctx, prepared.Provider.String(), prepared.ModelID, scannerFingerprint)
	if err != nil {
		return fmt.Errorf("begin file hash state: %w", err)
	}
	s.Logf("staged_file_hash_state_version=%d", fileHashStateVersion)

	s.Logf("state_dir=%s", targetPaths.LocalDir)
	s.Logf("index_db=%s", resolvedIndexPath)
	s.Logf("provider=%s", prepared.Provider)
	if prepared.ResolvedLib != "" {
		s.Logf("lib=%s", prepared.ResolvedLib)
	}
	s.Logf("model=%s", prepared.ResolvedModel)
	s.Logf("model_id=%s", prepared.ModelID)
	if prepared.ContextSize > 0 {
		s.Logf("ctx_size=%d", prepared.ContextSize)
	}
	s.Logf("root=%s", rootAbs)

	repo, err := indexdb.Open(ctx, resolvedIndexPath, prepared.Embedder.Dim())
	if err != nil {
		return err
	}
	defer func() {
		_ = repo.Close()
	}()

	service := indexing.Service{
		Scanner:  scanner,
		Repo:     repo,
		Embedder: prepared.Embedder,
		Hashes:   hashStore,
	}

	result, err := service.Run(ctx, indexing.Params{
		Root:                 rootAbs,
		GitignorePath:        resolvedGitignore,
		Excludes:             excludes,
		ContextPrefix:        *contextPrefix,
		CommentPrefix:        *commentPrefix,
		Provider:             prepared.Provider.String(),
		ModelID:              prepared.ModelID,
		CurrentFileHashes:    currentFileHashes,
		FileHashStateVersion: fileHashStateVersion,
	}, s)
	if err != nil {
		return err
	}

	manifest, err := semsearch.UpdateIndexManifest(targetPaths.LocalDir, resolvedIndexPath, result.Version)
	if err != nil {
		return fmt.Errorf("update manifest: %w", err)
	}
	s.Logf("manifest version=%d indexing_hash=%s", manifest.Version, manifest.IndexingHash)

	if err := hashStore.ActivateState(ctx, prepared.Provider.String(), prepared.ModelID, fileHashStateVersion); err != nil {
		return fmt.Errorf("activate file hash state: %w", err)
	}
	if err := hashStore.CleanupInactiveStates(ctx); err != nil {
		return fmt.Errorf("cleanup inactive file hash states: %w", err)
	}
	s.Logf("file_hash_state_version=%d", fileHashStateVersion)

	if result.IndexedSymbols == 0 {
		s.Finish("No symbols found")
		return nil
	}

	s.Finish(fmt.Sprintf("Indexed %d symbols in %d files", result.IndexedSymbols, result.IndexedFiles))
	return nil
}
