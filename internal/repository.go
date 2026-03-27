package internal

// @filectx: SQLite repository for comment metadata, versioning, and sqlite-vec nearest-neighbor search over embeddings.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type Repository struct {
	db  *sql.DB
	dim int
}

type SearchResult struct {
	Path        string
	Line        int
	CommentHash string
	Distance    float64
}

func NewRepository(db *sql.DB, dim int) *Repository {
	return &Repository{
		db:  db,
		dim: dim,
	}
}

// @search: repository schema stores current_version in meta, comment locations in comments, and embedding vectors in a vec0 virtual table named embeddings.
func (r *Repository) Init(ctx context.Context) error {
	const op = "internal.Repository.Init"

	stmts := []string{
		`
		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		);
		`,
		`
		INSERT INTO meta(key, value)
		VALUES ('current_version', 0)
		ON CONFLICT(key) DO NOTHING;
		`,
		`
		CREATE TABLE IF NOT EXISTS comments (
			path TEXT NOT NULL,
			line INTEGER NOT NULL,
			comment_hash TEXT NOT NULL,
			version INTEGER NOT NULL,
			PRIMARY KEY (path, line)
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_comments_version
		ON comments(version);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_comments_comment_hash
		ON comments(comment_hash);
		`,
	}

	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: exec schema: %w", op, err)
		}
	}

	vecStmt := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
			comment_hash TEXT PRIMARY KEY,
			embedding FLOAT[%d]
		);
	`, r.dim)

	if _, err := r.db.ExecContext(ctx, vecStmt); err != nil {
		return fmt.Errorf("%s: create embeddings vec0: %w", op, err)
	}

	return nil
}

func (r *Repository) CurrentVersion(ctx context.Context) (int64, error) {
	const op = "internal.Repository.CurrentVersion"

	var version int64
	err := r.db.QueryRowContext(
		ctx,
		`SELECT value FROM meta WHERE key = 'current_version'`,
	).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("%s: select current_version: %w", op, err)
	}

	return version, nil
}

func (r *Repository) WorkingVersion(ctx context.Context) (int64, error) {
	current, err := r.CurrentVersion(ctx)
	if err != nil {
		return 0, err
	}
	return current + 1, nil
}

func (r *Repository) ActivateVersion(ctx context.Context, version int64) error {
	const op = "internal.Repository.ActivateVersion"

	_, err := r.db.ExecContext(
		ctx,
		`UPDATE meta SET value = ? WHERE key = 'current_version'`,
		version,
	)
	if err != nil {
		return fmt.Errorf("%s: update current_version: %w", op, err)
	}

	return nil
}

func (r *Repository) EmbeddingExists(ctx context.Context, commentHash string) (bool, error) {
	const op = "internal.Repository.EmbeddingExists"

	var exists int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM embeddings WHERE comment_hash = ? LIMIT 1`,
		commentHash,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%s: select embedding: %w", op, err)
	}

	return true, nil
}

// @search: embeddings are keyed by comment hash so unchanged comments can reuse stored vectors across re-indexing runs.
func (r *Repository) AddEmbedding(ctx context.Context, commentHash string, embedding []float32) error {
	const op = "internal.Repository.AddEmbedding"

	if len(embedding) != r.dim {
		return fmt.Errorf("%s: invalid embedding dimension: got=%d want=%d", op, len(embedding), r.dim)
	}

	vec, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("%s: serialize embedding: %w", op, err)
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO embeddings(comment_hash, embedding)
		 VALUES (?, ?)`,
		commentHash,
		vec,
	)
	if err != nil {
		return fmt.Errorf("%s: insert embedding: %w", op, err)
	}

	return nil
}

func (r *Repository) UpsertComment(
	ctx context.Context,
	path string,
	line int,
	commentHash string,
	version int64,
) error {
	const op = "internal.Repository.UpsertComment"

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO comments(path, line, comment_hash, version)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(path, line) DO UPDATE SET
		   comment_hash = excluded.comment_hash,
		   version = excluded.version`,
		path,
		line,
		commentHash,
		version,
	)
	if err != nil {
		return fmt.Errorf("%s: upsert comment: %w", op, err)
	}

	return nil
}

// @search: each indexing run writes a new working version, activates it when complete, and deletes comment rows from older versions.
func (r *Repository) CleanupOldVersions(ctx context.Context, activeVersion int64) error {
	const op = "internal.Repository.CleanupOldVersions"

	_, err := r.db.ExecContext(
		ctx,
		`DELETE FROM comments WHERE version < ?`,
		activeVersion,
	)
	if err != nil {
		return fmt.Errorf("%s: delete old comments: %w", op, err)
	}

	return nil
}

func (r *Repository) CleanupUnusedEmbeddings(ctx context.Context) error {
	const op = "internal.Repository.CleanupUnusedEmbeddings"

	_, err := r.db.ExecContext(
		ctx,
		`DELETE FROM embeddings
		WHERE comment_hash NOT IN (
			SELECT DISTINCT comment_hash FROM comments
		)`,
	)
	if err != nil {
		return fmt.Errorf("%s: delete unused embeddings: %w", op, err)
	}

	return nil
}

func (r *Repository) SearchByVersion(
	ctx context.Context,
	queryEmbedding []float32,
	version int64,
	limit int,
) ([]SearchResult, error) {
	const op = "internal.Repository.SearchByVersion"

	if len(queryEmbedding) != r.dim {
		return nil, fmt.Errorf("%s: invalid query dimension: got=%d want=%d", op, len(queryEmbedding), r.dim)
	}
	if limit <= 0 {
		limit = 10
	}

	queryVec, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("%s: serialize query vector: %w", op, err)
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT
			c.path,
			c.line,
			c.comment_hash,
			e.distance
		FROM embeddings e
		JOIN comments c ON c.comment_hash = e.comment_hash
		WHERE e.embedding MATCH ?
		  AND k = ?
		  AND c.version = ?
		ORDER BY e.distance ASC`,
		queryVec,
		limit,
		version,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: search query: %w", op, err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var item SearchResult
		if err := rows.Scan(
			&item.Path,
			&item.Line,
			&item.CommentHash,
			&item.Distance,
		); err != nil {
			return nil, fmt.Errorf("%s: scan row: %w", op, err)
		}
		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows err: %w", op, err)
	}

	return results, nil
}

// @search: SearchCurrent resolves the active version and SearchByVersion performs the sqlite-vec MATCH query ordered by distance.
func (r *Repository) SearchCurrent(
	ctx context.Context,
	queryEmbedding []float32,
	limit int,
) ([]SearchResult, error) {
	version, err := r.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	return r.SearchByVersion(ctx, queryEmbedding, version, limit)
}

func (r *Repository) ListCommentsByVersion(ctx context.Context, version int64) ([]SearchResult, error) {
	const op = "internal.Repository.ListCommentsByVersion"

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT path, line, comment_hash
		FROM comments
		WHERE version = ?
		ORDER BY path ASC, line ASC`,
		version,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: list comments: %w", op, err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var item SearchResult
		if err := rows.Scan(&item.Path, &item.Line, &item.CommentHash); err != nil {
			return nil, fmt.Errorf("%s: scan row: %w", op, err)
		}
		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows err: %w", op, err)
	}

	return results, nil
}

func (r *Repository) ListCurrentComments(ctx context.Context) ([]SearchResult, error) {
	version, err := r.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	return r.ListCommentsByVersion(ctx, version)
}
