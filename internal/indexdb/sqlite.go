package indexdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/search"
)

var ErrNoActiveSnapshot = errors.New("no active snapshot for model")

// Store wraps the SQLite connection and vector dimension used by the index database.
type Store struct {
	db  *sql.DB
	dim int
}

// Open initializes the SQLite schema used for snapshot-based symbol metadata and vector search.
func Open(ctx context.Context, dbPath string, dim int) (*Store, error) {
	registerSQLiteVec()

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
		CREATE TABLE IF NOT EXISTS index_snapshots (
			snapshot_id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_index_snapshots_model_id
		ON index_snapshots(model_id, snapshot_id);
		`,
		`
		CREATE TABLE IF NOT EXISTS current_model_snapshots (
			model_id TEXT PRIMARY KEY,
			snapshot_id INTEGER NOT NULL
		);
		`,
		`
		CREATE TABLE IF NOT EXISTS snapshot_symbols (
			snapshot_id INTEGER NOT NULL,
			model_id TEXT NOT NULL,
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
			PRIMARY KEY (snapshot_id, path, symbol_id)
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_snapshot_symbols_snapshot_id
		ON snapshot_symbols(snapshot_id);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_snapshot_symbols_model_id
		ON snapshot_symbols(model_id, snapshot_id);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_snapshot_symbols_embedding_hash
		ON snapshot_symbols(model_id, embedding_hash);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_snapshot_symbols_qualified_name
		ON snapshot_symbols(model_id, qualified_name);
		`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}

	vecStmt := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS snapshot_embeddings USING vec0(
			embedding_key TEXT PRIMARY KEY,
			model_id TEXT NOT NULL,
			embedding_hash TEXT NOT NULL,
			embedding FLOAT[%d]
		);
	`, s.dim)
	if _, err := s.db.ExecContext(ctx, vecStmt); err != nil {
		return fmt.Errorf("create snapshot_embeddings vec0: %w", err)
	}

	return nil
}

// BeginSnapshot creates a new immutable snapshot id for one model family.
func (s *Store) BeginSnapshot(ctx context.Context, modelID string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO index_snapshots(model_id) VALUES (?)`, modelID)
	if err != nil {
		return 0, fmt.Errorf("insert snapshot: %w", err)
	}
	snapshotID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read snapshot id: %w", err)
	}
	return snapshotID, nil
}

// CurrentSnapshot returns the active snapshot for one model family.
func (s *Store) CurrentSnapshot(ctx context.Context, modelID string) (int64, error) {
	var snapshotID int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT snapshot_id FROM current_model_snapshots WHERE model_id = ?`,
		modelID,
	).Scan(&snapshotID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: %s", ErrNoActiveSnapshot, modelID)
		}
		return 0, fmt.Errorf("select current snapshot: %w", err)
	}
	return snapshotID, nil
}

// ActivateSnapshot marks one snapshot as the active searchable snapshot for its model family.
func (s *Store) ActivateSnapshot(ctx context.Context, modelID string, snapshotID int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO current_model_snapshots(model_id, snapshot_id)
		VALUES (?, ?)
		ON CONFLICT(model_id) DO UPDATE SET snapshot_id = excluded.snapshot_id`,
		modelID,
		snapshotID,
	)
	if err != nil {
		return fmt.Errorf("activate snapshot: %w", err)
	}
	return nil
}

// EmbeddingExists reports whether the vector table already contains the given embedding hash for one model.
func (s *Store) EmbeddingExists(ctx context.Context, modelID string, embeddingHash string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM snapshot_embeddings WHERE embedding_key = ? LIMIT 1`,
		embeddingKey(modelID, embeddingHash),
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("select embedding: %w", err)
	}
	return true, nil
}

// AddEmbedding stores a new embedding vector under its content hash for one model family.
func (s *Store) AddEmbedding(ctx context.Context, modelID string, embeddingHash string, embedding []float32) error {
	if len(embedding) != s.dim {
		return fmt.Errorf("invalid embedding dimension: got=%d want=%d", len(embedding), s.dim)
	}

	vec, err := serializeVector(embedding)
	if err != nil {
		return fmt.Errorf("serialize embedding: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO snapshot_embeddings(embedding_key, model_id, embedding_hash, embedding) VALUES (?, ?, ?, ?)`,
		embeddingKey(modelID, embeddingHash),
		modelID,
		embeddingHash,
		vec,
	)
	if err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}
	return nil
}

// InsertSymbol stores one symbol row inside a building snapshot without mutating active snapshots.
func (s *Store) InsertSymbol(ctx context.Context, snapshotID int64, modelID string, path string, symbol indexing.IndexedSymbol, embeddingHash string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO snapshot_symbols(
			snapshot_id,
			model_id,
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
			embedding_hash
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshotID,
		modelID,
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
	)
	if err != nil {
		return fmt.Errorf("insert symbol: %w", err)
	}
	return nil
}

// CleanupInactiveSnapshots removes building or stale snapshots that are not active for any model family.
func (s *Store) CleanupInactiveSnapshots(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM snapshot_symbols
		WHERE snapshot_id NOT IN (SELECT snapshot_id FROM current_model_snapshots)`,
	); err != nil {
		return fmt.Errorf("delete inactive snapshot symbols: %w", err)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM index_snapshots
		WHERE snapshot_id NOT IN (SELECT snapshot_id FROM current_model_snapshots)`,
	); err != nil {
		return fmt.Errorf("delete inactive snapshots: %w", err)
	}
	return nil
}

// CleanupUnusedEmbeddings removes embeddings that are no longer referenced by active or building snapshot rows.
func (s *Store) CleanupUnusedEmbeddings(ctx context.Context) error {
	_, err := s.db.ExecContext(
		ctx,
		`DELETE FROM snapshot_embeddings
		WHERE embedding_key NOT IN (
			SELECT DISTINCT model_id || ':' || embedding_hash FROM snapshot_symbols
		)`,
	)
	if err != nil {
		return fmt.Errorf("delete unused embeddings: %w", err)
	}
	return nil
}

// SearchBySnapshot performs semantic nearest-neighbor search against one immutable snapshot.
func (s *Store) SearchBySnapshot(ctx context.Context, modelID string, snapshotID int64, queryEmbedding []float32, limit int) ([]search.SearchResult, error) {
	if len(queryEmbedding) != s.dim {
		return nil, fmt.Errorf("invalid query dimension: got=%d want=%d", len(queryEmbedding), s.dim)
	}
	if limit <= 0 {
		limit = 10
	}

	queryVec, err := serializeVector(queryEmbedding)
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
		FROM snapshot_embeddings e
		JOIN snapshot_symbols s
		  ON s.model_id = e.model_id
		 AND s.embedding_hash = e.embedding_hash
		WHERE e.embedding MATCH ?
		  AND k = ?
		  AND s.model_id = ?
		  AND s.snapshot_id = ?
		ORDER BY e.distance ASC`,
		queryVec,
		limit,
		modelID,
		snapshotID,
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

// SearchCurrent performs semantic nearest-neighbor search against the active snapshot for one model.
func (s *Store) SearchCurrent(ctx context.Context, modelID string, queryEmbedding []float32, limit int) ([]search.SearchResult, error) {
	snapshotID, err := s.CurrentSnapshot(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return s.SearchBySnapshot(ctx, modelID, snapshotID, queryEmbedding, limit)
}

// ListSymbolsBySnapshot returns all symbols stored in one immutable snapshot.
func (s *Store) ListSymbolsBySnapshot(ctx context.Context, modelID string, snapshotID int64) ([]search.SearchResult, error) {
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
		FROM snapshot_symbols
		WHERE model_id = ?
		  AND snapshot_id = ?
		ORDER BY path ASC, line ASC, qualified_name ASC`,
		modelID,
		snapshotID,
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

// ListCurrentSymbols returns all symbols from the active snapshot for one model family.
func (s *Store) ListCurrentSymbols(ctx context.Context, modelID string) ([]search.SearchResult, error) {
	snapshotID, err := s.CurrentSnapshot(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return s.ListSymbolsBySnapshot(ctx, modelID, snapshotID)
}

func embeddingKey(modelID string, embeddingHash string) string {
	return modelID + ":" + embeddingHash
}
