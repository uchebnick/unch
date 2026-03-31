package indexdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"github.com/uchebnick/unch-searcher/internal/indexing"
	"github.com/uchebnick/unch-searcher/internal/search"
)

// Store wraps the SQLite connection and vector dimension used by the index database.
type Store struct {
	db  *sql.DB
	dim int
}

// Open initializes the SQLite schema used for symbol metadata and vector search.
func Open(ctx context.Context, dbPath string, dim int) (*Store, error) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	store := &Store{db: db, dim: dim}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init repository: %w", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
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
		CREATE TABLE IF NOT EXISTS symbols (
			path TEXT NOT NULL,
			line INTEGER NOT NULL,
			symbol_id TEXT NOT NULL,
			symbol_kind TEXT NOT NULL,
			symbol_name TEXT NOT NULL,
			symbol_container TEXT NOT NULL,
			qualified_name TEXT NOT NULL,
			signature TEXT NOT NULL,
			documentation TEXT NOT NULL,
			body TEXT NOT NULL,
			embedding_hash TEXT NOT NULL,
			version INTEGER NOT NULL,
			PRIMARY KEY (path, symbol_id)
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_symbols_version
		ON symbols(version);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_symbols_embedding_hash
		ON symbols(embedding_hash);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_symbols_qualified_name
		ON symbols(qualified_name);
		`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

	vecStmt := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
			comment_hash TEXT PRIMARY KEY,
			embedding FLOAT[%d]
		);
	`, s.dim)
	if _, err := s.db.ExecContext(ctx, vecStmt); err != nil {
		return fmt.Errorf("create embeddings vec0: %w", err)
	}

	return nil
}

// CurrentVersion returns the currently active logical index version.
func (s *Store) CurrentVersion(ctx context.Context) (int64, error) {
	var version int64
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'current_version'`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("select current_version: %w", err)
	}
	return version, nil
}

// WorkingVersion returns the next index version that should be written during reindexing.
func (s *Store) WorkingVersion(ctx context.Context) (int64, error) {
	current, err := s.CurrentVersion(ctx)
	if err != nil {
		return 0, err
	}
	return current + 1, nil
}

// ActivateVersion marks the provided version as the current searchable snapshot.
func (s *Store) ActivateVersion(ctx context.Context, version int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = 'current_version'`, version)
	if err != nil {
		return fmt.Errorf("update current_version: %w", err)
	}
	return nil
}

// EmbeddingExists reports whether the vector table already contains the given embedding hash.
func (s *Store) EmbeddingExists(ctx context.Context, embeddingHash string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM embeddings WHERE comment_hash = ? LIMIT 1`, embeddingHash).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("select embedding: %w", err)
	}
	return true, nil
}

// AddEmbedding stores a new embedding vector under its content hash.
func (s *Store) AddEmbedding(ctx context.Context, embeddingHash string, embedding []float32) error {
	if len(embedding) != s.dim {
		return fmt.Errorf("invalid embedding dimension: got=%d want=%d", len(embedding), s.dim)
	}

	vec, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO embeddings(comment_hash, embedding) VALUES (?, ?)`, embeddingHash, vec)
	if err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}
	return nil
}

// UpsertSymbol writes or updates the symbol metadata for one file entry in the working version.
func (s *Store) UpsertSymbol(ctx context.Context, path string, symbol indexing.IndexedSymbol, embeddingHash string, version int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO symbols(
			path,
			line,
			symbol_id,
			symbol_kind,
			symbol_name,
			symbol_container,
			qualified_name,
			signature,
			documentation,
			body,
			embedding_hash,
			version
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, symbol_id) DO UPDATE SET
			line = excluded.line,
			symbol_kind = excluded.symbol_kind,
			symbol_name = excluded.symbol_name,
			symbol_container = excluded.symbol_container,
			qualified_name = excluded.qualified_name,
			signature = excluded.signature,
			documentation = excluded.documentation,
			body = excluded.body,
			embedding_hash = excluded.embedding_hash,
			version = excluded.version`,
		path,
		symbol.Line,
		symbol.StableID(),
		symbol.Kind,
		symbol.Name,
		symbol.Container,
		symbol.QualifiedName,
		symbol.Signature,
		symbol.Documentation,
		symbol.Body,
		embeddingHash,
		version,
	)
	if err != nil {
		return fmt.Errorf("upsert symbol: %w", err)
	}
	return nil
}

// CleanupOldVersions removes symbol rows from previous index generations.
func (s *Store) CleanupOldVersions(ctx context.Context, activeVersion int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM symbols WHERE version < ?`, activeVersion)
	if err != nil {
		return fmt.Errorf("delete old symbols: %w", err)
	}
	return nil
}

// CleanupUnusedEmbeddings removes embeddings that are no longer referenced by active symbols.
func (s *Store) CleanupUnusedEmbeddings(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM embeddings
		WHERE comment_hash NOT IN (
			SELECT DISTINCT embedding_hash FROM symbols
		)`)
	if err != nil {
		return fmt.Errorf("delete unused embeddings: %w", err)
	}
	return nil
}

// SearchByVersion performs semantic nearest-neighbor search against one logical index version.
func (s *Store) SearchByVersion(ctx context.Context, queryEmbedding []float32, version int64, limit int) ([]search.SearchResult, error) {
	if len(queryEmbedding) != s.dim {
		return nil, fmt.Errorf("invalid query dimension: got=%d want=%d", len(queryEmbedding), s.dim)
	}
	if limit <= 0 {
		limit = 10
	}

	queryVec, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("serialize query vector: %w", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			s.path,
			s.line,
			s.symbol_id,
			s.symbol_kind,
			s.symbol_name,
			s.symbol_container,
			s.qualified_name,
			s.signature,
			s.documentation,
			s.body,
			e.distance
		FROM embeddings e
		JOIN symbols s ON s.embedding_hash = e.comment_hash
		WHERE e.embedding MATCH ?
		  AND k = ?
		  AND s.version = ?
		ORDER BY e.distance ASC`,
		queryVec,
		limit,
		version,
	)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	results := make([]search.SearchResult, 0, limit)
	for rows.Next() {
		var item search.SearchResult
		if err := rows.Scan(
			&item.Path,
			&item.Line,
			&item.SymbolID,
			&item.Kind,
			&item.Name,
			&item.Container,
			&item.QualifiedName,
			&item.Signature,
			&item.Documentation,
			&item.Body,
			&item.Distance,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return results, nil
}

// SearchCurrent performs semantic nearest-neighbor search against the active index version.
func (s *Store) SearchCurrent(ctx context.Context, queryEmbedding []float32, limit int) ([]search.SearchResult, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	return s.SearchByVersion(ctx, queryEmbedding, version, limit)
}

// ListSymbolsByVersion returns all symbols stored in a specific index version.
func (s *Store) ListSymbolsByVersion(ctx context.Context, version int64) ([]search.SearchResult, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			path,
			line,
			symbol_id,
			symbol_kind,
			symbol_name,
			symbol_container,
			qualified_name,
			signature,
			documentation,
			body
		FROM symbols
		WHERE version = ?
		ORDER BY path ASC, line ASC, qualified_name ASC`,
		version,
	)
	if err != nil {
		return nil, fmt.Errorf("list symbols: %w", err)
	}
	defer rows.Close()

	var results []search.SearchResult
	for rows.Next() {
		var item search.SearchResult
		if err := rows.Scan(
			&item.Path,
			&item.Line,
			&item.SymbolID,
			&item.Kind,
			&item.Name,
			&item.Container,
			&item.QualifiedName,
			&item.Signature,
			&item.Documentation,
			&item.Body,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	return results, nil
}

// ListCurrentSymbols returns all symbols from the active index version for lexical search.
func (s *Store) ListCurrentSymbols(ctx context.Context) ([]search.SearchResult, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListSymbolsByVersion(ctx, version)
}
