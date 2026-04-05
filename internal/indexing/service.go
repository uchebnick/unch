package indexing

import (
	"context"
	"fmt"
)

type Reporter interface {
	Logf(format string, args ...any)
	CountProgress(label string, current, total int)
}

type Scanner interface {
	CollectJob(path string, rel string, source []byte, commentPrefix string, contextPrefix string) (FileJob, bool, error)
}

type Repository interface {
	BeginSnapshot(ctx context.Context, modelID string) (int64, error)
	CurrentSnapshotIfAny(ctx context.Context, modelID string) (int64, bool, error)
	ActivateSnapshot(ctx context.Context, modelID string, snapshotID int64) error
	EmbeddingExists(ctx context.Context, modelID string, embeddingHash string) (bool, error)
	AddEmbedding(ctx context.Context, modelID string, embeddingHash string, embedding []float32) error
	InsertSymbol(ctx context.Context, snapshotID int64, modelID string, path string, symbol IndexedSymbol, embeddingHash string) error
	CopyPathFromSnapshot(ctx context.Context, modelID string, srcSnapshotID, dstSnapshotID int64, path string) (int, error)
	CleanupInactiveSnapshots(ctx context.Context) error
	CleanupUnusedEmbeddings(ctx context.Context) error
}

type FileHashStore interface {
	InsertFileHash(ctx context.Context, stateVersion int64, path string, contentHash string) error
}

type Embedder interface {
	IndexedSymbolHash(path string, symbol IndexedSymbol) string
	EmbedIndexedSymbol(path string, symbol IndexedSymbol) ([]float32, error)
}

type Params struct {
	Root                 string
	GitignorePath        string
	Excludes             []string
	ContextPrefix        string
	CommentPrefix        string
	ModelID              string
	CurrentFileHashes    map[string]string
	FileHashStateVersion int64
}

type Result struct {
	Version        int64
	IndexedFiles   int
	IndexedSymbols int
}

type Service struct {
	Scanner  Scanner
	Repo     Repository
	Embedder Embedder
	Hashes   FileHashStore
}

type runState struct {
	service            Service
	ctx                context.Context
	params             Params
	reporter           Reporter
	modelID            string
	currentSnapshotID  int64
	hasCurrentSnapshot bool
	snapshotID         int64
	totalFiles         int
	processedFiles     int
	reusedFiles        int
	reusedSymbols      int
	result             Result
}

// Run scans the repository, embeds extracted symbols, and activates the new index version.
func (s Service) Run(ctx context.Context, params Params, reporter Reporter) (Result, error) {
	modelID := params.ModelID
	if modelID == "" {
		modelID = "embeddinggemma"
	}

	currentSnapshotID, hasCurrentSnapshot, err := s.Repo.CurrentSnapshotIfAny(ctx, modelID)
	if err != nil {
		return Result{}, fmt.Errorf("read current snapshot: %w", err)
	}

	snapshotID, err := s.Repo.BeginSnapshot(ctx, modelID)
	if err != nil {
		return Result{}, fmt.Errorf("begin snapshot: %w", err)
	}
	if reporter != nil {
		reporter.Logf("model=%s", modelID)
		reporter.Logf("snapshot id=%d", snapshotID)
	}

	state := runState{
		service:            s,
		ctx:                ctx,
		params:             params,
		reporter:           reporter,
		modelID:            modelID,
		currentSnapshotID:  currentSnapshotID,
		hasCurrentSnapshot: hasCurrentSnapshot,
		snapshotID:         snapshotID,
		totalFiles:         len(params.CurrentFileHashes),
		result:             Result{Version: snapshotID},
	}

	if err := state.walk(); err != nil {
		return Result{}, err
	}
	if err := state.finalize(); err != nil {
		return Result{}, err
	}

	return state.result, nil
}

func (r *runState) walk() error {
	return walkIndexedPaths(r.params.Root, r.params.GitignorePath, r.params.Excludes, r.handleFile)
}

func (r *runState) handleFile(path string, rel string) error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	default:
	}

	contentHash, binary, err := hashSourceFile(path)
	if err != nil {
		return fmt.Errorf("hash source for %s: %w", rel, err)
	}
	if binary {
		return nil
	}

	if err := r.storeFileHash(rel, contentHash); err != nil {
		return err
	}
	if r.canReuseFile(rel, contentHash) {
		return r.reuseFile(rel)
	}
	return r.reindexFile(path, rel)
}

func (r *runState) storeFileHash(rel string, contentHash string) error {
	if r.service.Hashes == nil || r.params.FileHashStateVersion <= 0 {
		return nil
	}
	if err := r.service.Hashes.InsertFileHash(r.ctx, r.params.FileHashStateVersion, rel, contentHash); err != nil {
		return fmt.Errorf("insert file hash for %s: %w", rel, err)
	}
	return nil
}

func (r *runState) canReuseFile(rel string, contentHash string) bool {
	return r.hasCurrentSnapshot && r.params.CurrentFileHashes[rel] == contentHash
}

func (r *runState) reuseFile(rel string) error {
	copiedSymbols, err := r.service.Repo.CopyPathFromSnapshot(r.ctx, r.modelID, r.currentSnapshotID, r.snapshotID, rel)
	if err != nil {
		return fmt.Errorf("copy unchanged file %s: %w", rel, err)
	}
	if copiedSymbols > 0 {
		r.reusedFiles++
		r.reusedSymbols += copiedSymbols
		r.result.IndexedFiles++
		r.result.IndexedSymbols += copiedSymbols
	}
	r.advanceProgress()
	return nil
}

func (r *runState) reindexFile(path string, rel string) error {
	source, binary, err := readSourceFile(path)
	if err != nil {
		return fmt.Errorf("read source for %s: %w", rel, err)
	}
	if binary {
		return nil
	}

	job, ok, err := r.service.Scanner.CollectJob(path, rel, source, r.params.CommentPrefix, r.params.ContextPrefix)
	if err != nil {
		return err
	}
	if !ok {
		r.advanceProgress()
		return nil
	}

	if err := r.indexJob(job); err != nil {
		return err
	}
	r.result.IndexedFiles++
	r.result.IndexedSymbols += len(job.Symbols)
	r.advanceProgress()
	return nil
}

func (r *runState) indexJob(job FileJob) error {
	for _, symbol := range job.Symbols {
		hash := r.service.Embedder.IndexedSymbolHash(job.Path, symbol)

		exists, err := r.service.Repo.EmbeddingExists(r.ctx, r.modelID, hash)
		if err != nil {
			return fmt.Errorf("check embedding exists: %w", err)
		}
		if !exists {
			vec, err := r.service.Embedder.EmbedIndexedSymbol(job.Path, symbol)
			if err != nil {
				return fmt.Errorf("embed symbol at %s:%d: %w", job.Path, symbol.Line, err)
			}
			if err := r.service.Repo.AddEmbedding(r.ctx, r.modelID, hash, vec); err != nil {
				return fmt.Errorf("store embedding: %w", err)
			}
		}

		if err := r.service.Repo.InsertSymbol(r.ctx, r.snapshotID, r.modelID, job.Path, symbol, hash); err != nil {
			return fmt.Errorf("insert symbol: %w", err)
		}
	}
	return nil
}

func (r *runState) advanceProgress() {
	r.processedFiles++
	if r.processedFiles > r.totalFiles {
		r.totalFiles = r.processedFiles
	}
	if r.reporter != nil {
		r.reporter.CountProgress("Indexing", r.processedFiles, r.totalFiles)
	}
}

func (r *runState) finalize() error {
	if r.processedFiles > 0 && r.reporter != nil {
		r.reporter.CountProgress("Indexing", r.processedFiles, r.processedFiles)
	}
	if err := r.service.Repo.ActivateSnapshot(r.ctx, r.modelID, r.snapshotID); err != nil {
		return fmt.Errorf("activate snapshot: %w", err)
	}
	if err := r.service.Repo.CleanupInactiveSnapshots(r.ctx); err != nil {
		return fmt.Errorf("cleanup inactive snapshots: %w", err)
	}
	if err := r.service.Repo.CleanupUnusedEmbeddings(r.ctx); err != nil {
		return fmt.Errorf("cleanup unused embeddings: %w", err)
	}
	if r.reporter != nil {
		r.reporter.Logf("reused files=%d", r.reusedFiles)
		r.reporter.Logf("reused symbols=%d", r.reusedSymbols)
		r.reporter.Logf("indexing completed")
	}
	return nil
}
