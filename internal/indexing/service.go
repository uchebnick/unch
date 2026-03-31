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
	WorkingVersion(ctx context.Context) (int64, error)
	ActivateVersion(ctx context.Context, version int64) error
	EmbeddingExists(ctx context.Context, embeddingHash string) (bool, error)
	AddEmbedding(ctx context.Context, embeddingHash string, embedding []float32) error
	UpsertSymbol(ctx context.Context, path string, symbol IndexedSymbol, embeddingHash string, version int64) error
	CleanupOldVersions(ctx context.Context, activeVersion int64) error
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

	workingVersion, err := s.Repo.WorkingVersion(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("get working version: %w", err)
	}
	if reporter != nil {
		reporter.Logf("working version=%d", workingVersion)
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

			exists, err := s.Repo.EmbeddingExists(ctx, hash)
			if err != nil {
				return Result{}, fmt.Errorf("check embedding exists: %w", err)
			}
			if !exists {
				if err := s.Repo.AddEmbedding(ctx, hash, vec); err != nil {
					return Result{}, fmt.Errorf("store embedding: %w", err)
				}
			}

			if err := s.Repo.UpsertSymbol(ctx, job.Path, symbol, hash, workingVersion); err != nil {
				return Result{}, fmt.Errorf("upsert symbol: %w", err)
			}
		}

		processed += len(job.Symbols)
		if reporter != nil {
			reporter.CountProgress("Indexing", processed, totalSymbols)
		}
	}

	if err := s.Repo.ActivateVersion(ctx, workingVersion); err != nil {
		return Result{}, fmt.Errorf("activate version: %w", err)
	}
	if err := s.Repo.CleanupOldVersions(ctx, workingVersion); err != nil {
		return Result{}, fmt.Errorf("cleanup old versions: %w", err)
	}
	if err := s.Repo.CleanupUnusedEmbeddings(ctx); err != nil {
		return Result{}, fmt.Errorf("cleanup unused embeddings: %w", err)
	}
	if reporter != nil {
		reporter.Logf("indexing completed")
	}

	return Result{
		Version:        workingVersion,
		IndexedFiles:   len(jobs),
		IndexedSymbols: totalSymbols,
	}, nil
}
