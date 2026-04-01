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
	CollectJobs(root string, gitignorePath string, extraPatterns []string, commentPrefix string, contextPrefix string) ([]FileJob, int, error)
}

type Repository interface {
	BeginSnapshot(ctx context.Context, modelID string) (int64, error)
	ActivateSnapshot(ctx context.Context, modelID string, snapshotID int64) error
	EmbeddingExists(ctx context.Context, modelID string, embeddingHash string) (bool, error)
	AddEmbedding(ctx context.Context, modelID string, embeddingHash string, embedding []float32) error
	InsertSymbol(ctx context.Context, snapshotID int64, modelID string, path string, symbol IndexedSymbol, embeddingHash string) error
	CleanupInactiveSnapshots(ctx context.Context) error
	CleanupUnusedEmbeddings(ctx context.Context) error
}

type Embedder interface {
	EmbedIndexedSymbol(path string, symbol IndexedSymbol) (string, []float32, error)
}

type Params struct {
	Root          string
	GitignorePath string
	Excludes      []string
	ContextPrefix string
	CommentPrefix string
	ModelID       string
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
}

// Run scans the repository, embeds extracted symbols, and activates the new index version.
func (s Service) Run(ctx context.Context, params Params, reporter Reporter) (Result, error) {
	jobs, totalSymbols, err := s.Scanner.CollectJobs(
		params.Root,
		params.GitignorePath,
		params.Excludes,
		params.CommentPrefix,
		params.ContextPrefix,
	)
	if err != nil {
		return Result{}, fmt.Errorf("collect jobs: %w", err)
	}
	if reporter != nil {
		reporter.Logf("files to index=%d", len(jobs))
		reporter.Logf("symbols to index=%d", totalSymbols)
	}

	modelID := params.ModelID
	if modelID == "" {
		modelID = "embeddinggemma"
	}

	snapshotID, err := s.Repo.BeginSnapshot(ctx, modelID)
	if err != nil {
		return Result{}, fmt.Errorf("begin snapshot: %w", err)
	}
	if reporter != nil {
		reporter.Logf("model=%s", modelID)
		reporter.Logf("snapshot id=%d", snapshotID)
	}

	processed := 0
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}

		for _, symbol := range job.Symbols {
			hash, vec, err := s.Embedder.EmbedIndexedSymbol(job.Path, symbol)
			if err != nil {
				return Result{}, fmt.Errorf("embed symbol at %s:%d: %w", job.Path, symbol.Line, err)
			}

			exists, err := s.Repo.EmbeddingExists(ctx, modelID, hash)
			if err != nil {
				return Result{}, fmt.Errorf("check embedding exists: %w", err)
			}
			if !exists {
				if err := s.Repo.AddEmbedding(ctx, modelID, hash, vec); err != nil {
					return Result{}, fmt.Errorf("store embedding: %w", err)
				}
			}

			if err := s.Repo.InsertSymbol(ctx, snapshotID, modelID, job.Path, symbol, hash); err != nil {
				return Result{}, fmt.Errorf("insert symbol: %w", err)
			}
		}

		processed += len(job.Symbols)
		if reporter != nil {
			reporter.CountProgress("Indexing", processed, totalSymbols)
		}
	}

	if err := s.Repo.ActivateSnapshot(ctx, modelID, snapshotID); err != nil {
		return Result{}, fmt.Errorf("activate snapshot: %w", err)
	}
	if err := s.Repo.CleanupInactiveSnapshots(ctx); err != nil {
		return Result{}, fmt.Errorf("cleanup inactive snapshots: %w", err)
	}
	if err := s.Repo.CleanupUnusedEmbeddings(ctx); err != nil {
		return Result{}, fmt.Errorf("cleanup unused embeddings: %w", err)
	}
	if reporter != nil {
		reporter.Logf("indexing completed")
	}

	return Result{
		Version:        snapshotID,
		IndexedFiles:   len(jobs),
		IndexedSymbols: totalSymbols,
	}, nil
}
