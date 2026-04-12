package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/filehashdb"
	"github.com/uchebnick/unch/internal/indexdb"
	"github.com/uchebnick/unch/internal/indexing"
	unchmcp "github.com/uchebnick/unch/internal/mcp"
	"github.com/uchebnick/unch/internal/runtime"
	appsearch "github.com/uchebnick/unch/internal/search"
	"github.com/uchebnick/unch/internal/semsearch"
)

type mcpBackendConfig struct {
	RootAbs           string
	TargetPaths       semsearch.Paths
	IndexPath         string
	RequestedProvider string
	RequestedModel    string
	RequestedLibPath  string
	ContextSize       int
	Verbose           bool
}

type preparedMCPResources struct {
	embedder          appembed.Embedder
	repo              *indexdb.Store
	provider          appembed.Provider
	modelID           string
	resolvedModel     string
	resolvedLibPath   string
	contextSize       int
}

type mcpBackend struct {
	cfg      mcpBackendConfig
	scanner  indexing.FileScanner
	models   runtime.ModelCache
	runtimes runtime.YzmaResolver

	runMu sync.Mutex
	mu    sync.Mutex

	prepared *preparedMCPResources
}

func newMCPBackend(cfg mcpBackendConfig) *mcpBackend {
	return &mcpBackend{cfg: cfg}
}

func (b *mcpBackend) Version() string {
	return versionString()
}

func (b *mcpBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var firstErr error
	if b.prepared != nil {
		if b.prepared.repo != nil {
			if err := b.prepared.repo.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if b.prepared.embedder != nil {
			b.prepared.embedder.Close()
		}
	}
	b.prepared = nil
	return firstErr
}

func (b *mcpBackend) WorkspaceStatus(_ context.Context) (unchmcp.WorkspaceStatusResult, error) {
	result := unchmcp.WorkspaceStatusResult{
		Root:           b.cfg.RootAbs,
		StateDir:       b.cfg.TargetPaths.LocalDir,
		IndexDB:        b.cfg.IndexPath,
		RequestedModel: b.cfg.RequestedModel,
		RequestedLib:   b.cfg.RequestedLibPath,
		RequestedProvider: b.cfg.RequestedProvider,
		ContextSize:    b.cfg.ContextSize,
	}

	if info, err := os.Stat(b.cfg.IndexPath); err == nil && !info.IsDir() {
		result.IndexPresent = true
	}

	if manifest, err := semsearch.ReadManifest(b.cfg.TargetPaths.LocalDir); err == nil {
		result.ManifestVersion = manifest.Version
		result.ManifestSource = manifest.Source
		if manifest.Remote != nil {
			result.RemoteCIURL = manifest.Remote.CIURL
		}
	}

	b.mu.Lock()
	if b.prepared != nil {
		result.Provider = b.prepared.provider.String()
		result.ModelID = b.prepared.modelID
		result.ResolvedModel = b.prepared.resolvedModel
		result.ResolvedLib = b.prepared.resolvedLibPath
		if result.ContextSize <= 0 {
			result.ContextSize = b.prepared.contextSize
		}
	}
	b.mu.Unlock()

	return result, nil
}

func (b *mcpBackend) SearchCode(ctx context.Context, params unchmcp.SearchCodeParams) (unchmcp.SearchCodeResult, error) {
	b.runMu.Lock()
	defer b.runMu.Unlock()

	mode, err := appsearch.NormalizeMode(params.Mode)
	if err != nil {
		return unchmcp.SearchCodeResult{}, err
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}

	prepared, err := b.ensurePrepared(ctx)
	if err != nil {
		return unchmcp.SearchCodeResult{}, err
	}

	if _, err := semsearch.EnsureFileWeights(b.cfg.TargetPaths.LocalDir); err != nil {
		return unchmcp.SearchCodeResult{}, err
	}
	fileWeights, err := semsearch.LoadFileWeights(b.cfg.TargetPaths.LocalDir)
	if err != nil {
		return unchmcp.SearchCodeResult{}, err
	}

	service := appsearch.Service{
		Repo:         prepared.repo,
		Embedder:     prepared.embedder,
		PathWeighter: fileWeights,
	}

	maxDistance := 0.85
	if params.MaxDistance != nil {
		maxDistance = *params.MaxDistance
	}

	results, err := service.Run(ctx, appsearch.Params{
		QueryText:   strings.TrimSpace(params.Query),
		Limit:       params.Limit,
		Mode:        mode,
		MaxDistance: maxDistance,
		Provider:    prepared.provider.String(),
		ModelID:     prepared.modelID,
	}, nil)
	if err != nil {
		return unchmcp.SearchCodeResult{}, err
	}

	hits := make([]unchmcp.SearchHit, 0, len(results))
	for _, result := range results {
		hit := unchmcp.SearchHit{
			Path:          formatRelativeToRoot(b.cfg.RootAbs, result.Path),
			Line:          result.Line,
			Metric:        result.DisplayMetric,
			Kind:          result.Kind,
			Name:          result.Name,
			QualifiedName: result.QualifiedName,
			Signature:     result.Signature,
			Distance:      result.Distance,
		}
		if params.Details {
			hit.Documentation = result.Documentation
			hit.Body = result.Body
		}
		hits = append(hits, hit)
	}

	return unchmcp.SearchCodeResult{
		Query:       strings.TrimSpace(params.Query),
		Mode:        mode,
		Provider:    prepared.provider.String(),
		ModelID:     prepared.modelID,
		StateDir:    b.cfg.TargetPaths.LocalDir,
		ResultCount: len(hits),
		Hits:        hits,
	}, nil
}

func (b *mcpBackend) IndexRepository(ctx context.Context, params unchmcp.IndexRepositoryParams) (unchmcp.IndexRepositoryResult, error) {
	b.runMu.Lock()
	defer b.runMu.Unlock()

	prepared, err := b.ensurePrepared(ctx)
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, err
	}

	if _, err := semsearch.EnsureFileWeights(b.cfg.TargetPaths.LocalDir); err != nil {
		return unchmcp.IndexRepositoryResult{}, err
	}

	commentPrefix := strings.TrimSpace(params.CommentPrefix)
	if commentPrefix == "" {
		commentPrefix = "@search:"
	}
	contextPrefix := strings.TrimSpace(params.ContextPrefix)
	if contextPrefix == "" {
		contextPrefix = "@filectx:"
	}

	resolvedGitignore, err := indexing.ResolveGitignorePath(b.cfg.RootAbs, strings.TrimSpace(params.Gitignore))
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("resolve gitignore: %w", err)
	}

	hashStore, err := filehashdb.Open(ctx, b.cfg.TargetPaths.FileHashDB)
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("open file hash db: %w", err)
	}
	defer func() {
		_ = hashStore.Close()
	}()

	scannerFingerprint := indexing.BuildScannerFingerprint(commentPrefix, contextPrefix, params.Excludes)

	var currentFileHashes map[string]string
	if currentState, ok, err := hashStore.Current(ctx, prepared.provider.String(), prepared.modelID); err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("read current file hash state: %w", err)
	} else if ok && currentState.ScannerFingerprint == scannerFingerprint {
		currentFileHashes = currentState.Files
	}

	fileHashStateVersion, err := hashStore.BeginState(ctx, prepared.provider.String(), prepared.modelID, scannerFingerprint)
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("begin file hash state: %w", err)
	}

	scanner := b.scanner
	scanner.Root = b.cfg.RootAbs

	service := indexing.Service{
		Scanner:  scanner,
		Repo:     prepared.repo,
		Embedder: prepared.embedder,
		Hashes:   hashStore,
	}

	manifest, manifestErr := semsearch.ReadManifest(b.cfg.TargetPaths.LocalDir)
	detachedRemoteBinding := manifestErr == nil && semsearch.HasRemoteBinding(manifest)

	result, err := service.Run(ctx, indexing.Params{
		Root:                 b.cfg.RootAbs,
		GitignorePath:        resolvedGitignore,
		Excludes:             params.Excludes,
		ContextPrefix:        contextPrefix,
		CommentPrefix:        commentPrefix,
		Provider:             prepared.provider.String(),
		ModelID:              prepared.modelID,
		CurrentFileHashes:    currentFileHashes,
		FileHashStateVersion: fileHashStateVersion,
	}, nil)
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, err
	}

	nextManifest, err := semsearch.UpdateIndexManifest(b.cfg.TargetPaths.LocalDir, b.cfg.IndexPath, result.Version)
	if err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("update manifest: %w", err)
	}
	if err := hashStore.ActivateState(ctx, prepared.provider.String(), prepared.modelID, fileHashStateVersion); err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("activate file hash state: %w", err)
	}
	if err := hashStore.CleanupInactiveStates(ctx); err != nil {
		return unchmcp.IndexRepositoryResult{}, fmt.Errorf("cleanup inactive file hash states: %w", err)
	}

	return unchmcp.IndexRepositoryResult{
		Provider:              prepared.provider.String(),
		ModelID:               prepared.modelID,
		StateDir:              b.cfg.TargetPaths.LocalDir,
		Version:               nextManifest.Version,
		IndexedFiles:          result.IndexedFiles,
		IndexedSymbols:        result.IndexedSymbols,
		DetachedRemoteBinding: detachedRemoteBinding,
	}, nil
}

func (b *mcpBackend) ensurePrepared(ctx context.Context) (*preparedMCPResources, error) {
	b.mu.Lock()
	if b.prepared != nil {
		prepared := b.prepared
		b.mu.Unlock()
		return prepared, nil
	}
	b.mu.Unlock()

	preparedEmbedder, err := prepareEmbedder(
		ctx,
		nil,
		b.cfg.TargetPaths,
		b.cfg.RequestedProvider,
		b.cfg.RequestedModel,
		b.cfg.RequestedLibPath,
		b.cfg.ContextSize,
		b.cfg.Verbose,
		b.runtimes,
		b.models,
	)
	if err != nil {
		return nil, fmt.Errorf("load embedder: %w", err)
	}

	repo, err := indexdb.Open(ctx, b.cfg.IndexPath, preparedEmbedder.Embedder.Dim())
	if err != nil {
		preparedEmbedder.Embedder.Close()
		return nil, err
	}

	prepared := &preparedMCPResources{
		embedder:          preparedEmbedder.Embedder,
		repo:              repo,
		provider:          preparedEmbedder.Provider,
		modelID:           preparedEmbedder.ModelID,
		resolvedModel:     preparedEmbedder.ResolvedModel,
		resolvedLibPath:   preparedEmbedder.ResolvedLib,
		contextSize:       preparedEmbedder.ContextSize,
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.prepared != nil {
		_ = repo.Close()
		preparedEmbedder.Embedder.Close()
		return b.prepared, nil
	}
	b.prepared = prepared
	return prepared, nil
}

func formatRelativeToRoot(root string, target string) string {
	if !filepath.IsAbs(target) {
		return filepath.ToSlash(filepath.Clean(target))
	}

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
