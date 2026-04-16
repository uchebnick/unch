package filehashdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const SchemaVersion = 3

type Store struct {
	db *sql.DB
}

type State struct {
	Version            int64
	Provider           string
	ModelID            string
	ScannerFingerprint string
	Files              map[string]string
}

func Open(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init db: %w", err)
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
	var userVersion int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	switch userVersion {
	case 0, SchemaVersion:
	case 1, 2:
		if err := s.resetSchema(ctx); err != nil {
			return fmt.Errorf("reset legacy schema: %w", err)
		}
	default:
		return fmt.Errorf("unsupported schema version %d", userVersion)
	}

	stmts := []string{
		`
		CREATE TABLE IF NOT EXISTS file_hash_states (
			state_version INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			model_id TEXT NOT NULL,
			scanner_fingerprint TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_file_hash_states_provider_model_id
		ON file_hash_states(provider, model_id, state_version);
		`,
		`
		CREATE TABLE IF NOT EXISTS current_model_states (
			provider TEXT NOT NULL,
			model_id TEXT NOT NULL,
			state_version INTEGER NOT NULL,
			PRIMARY KEY (provider, model_id)
		);
		`,
		`
		CREATE TABLE IF NOT EXISTS file_hashes (
			state_version INTEGER NOT NULL,
			path TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			PRIMARY KEY (state_version, path)
		);
		`,
		`
		CREATE INDEX IF NOT EXISTS idx_file_hashes_state_version
		ON file_hashes(state_version);
		`,
		fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion),
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}
	return nil
}

func (s *Store) resetSchema(ctx context.Context) error {
	stmts := []string{
		`DROP TABLE IF EXISTS current_model_states;`,
		`DROP TABLE IF EXISTS file_hash_states;`,
		`DROP TABLE IF EXISTS state_meta;`,
		`DROP TABLE IF EXISTS file_hashes;`,
		`PRAGMA user_version = 0`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Current(ctx context.Context, provider string, modelID string) (State, bool, error) {
	var state State
	state.Provider = provider
	state.ModelID = modelID

	err := s.db.QueryRowContext(
		ctx,
		`SELECT fs.scanner_fingerprint, fs.state_version
		 FROM current_model_states cms
		 JOIN file_hash_states fs
		   ON fs.state_version = cms.state_version
		 WHERE cms.provider = ?
		   AND cms.model_id = ?
		   AND fs.provider = ?
		   AND fs.model_id = ?`,
		provider,
		modelID,
		provider,
		modelID,
	).Scan(&state.ScannerFingerprint, &state.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return State{}, false, nil
		}
		return State{}, false, fmt.Errorf("select current state: %w", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT path, content_hash FROM file_hashes WHERE state_version = ? ORDER BY path`,
		state.Version,
	)
	if err != nil {
		return State{}, false, fmt.Errorf("select file hashes: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	state.Files = make(map[string]string)
	for rows.Next() {
		var path string
		var contentHash string
		if err := rows.Scan(&path, &contentHash); err != nil {
			return State{}, false, fmt.Errorf("scan file hash row: %w", err)
		}
		state.Files[path] = contentHash
	}
	if err := rows.Err(); err != nil {
		return State{}, false, fmt.Errorf("iterate file hash rows: %w", err)
	}

	return state, true, nil
}

func (s *Store) Matches(ctx context.Context, provider string, modelID string, scannerFingerprint string, files map[string]string) (bool, int64, error) {
	state, ok, err := s.Current(ctx, provider, modelID)
	if err != nil {
		return false, 0, err
	}
	if !ok {
		return false, 0, nil
	}
	if state.ScannerFingerprint != scannerFingerprint {
		return false, state.Version, nil
	}
	if len(state.Files) != len(files) {
		return false, state.Version, nil
	}
	for path, contentHash := range files {
		if state.Files[path] != contentHash {
			return false, state.Version, nil
		}
	}
	return true, state.Version, nil
}

func (s *Store) BeginState(ctx context.Context, provider string, modelID string, scannerFingerprint string) (int64, error) {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO file_hash_states(provider, model_id, scanner_fingerprint) VALUES (?, ?, ?)`,
		provider,
		modelID,
		scannerFingerprint,
	)
	if err != nil {
		return 0, fmt.Errorf("insert state: %w", err)
	}

	stateVersion, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read state version: %w", err)
	}
	return stateVersion, nil
}

func (s *Store) InsertFileHash(ctx context.Context, stateVersion int64, path string, contentHash string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO file_hashes(state_version, path, content_hash) VALUES (?, ?, ?)`,
		stateVersion,
		path,
		contentHash,
	)
	if err != nil {
		return fmt.Errorf("insert file hash for %s: %w", path, err)
	}
	return nil
}

func (s *Store) StageState(ctx context.Context, provider string, modelID string, scannerFingerprint string, files map[string]string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO file_hash_states(provider, model_id, scanner_fingerprint) VALUES (?, ?, ?)`,
		provider,
		modelID,
		scannerFingerprint,
	)
	if err != nil {
		return 0, fmt.Errorf("insert state: %w", err)
	}

	stateVersion, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read state version: %w", err)
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	stmt, err := tx.PrepareContext(
		ctx,
		`INSERT INTO file_hashes(state_version, path, content_hash) VALUES (?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare file hash insert: %w", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, path := range paths {
		if _, err := stmt.ExecContext(ctx, stateVersion, path, files[path]); err != nil {
			return 0, fmt.Errorf("insert file hash for %s: %w", path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	return stateVersion, nil
}

func (s *Store) ActivateState(ctx context.Context, provider string, modelID string, stateVersion int64) error {
	var storedProvider string
	var storedModelID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT provider, model_id FROM file_hash_states WHERE state_version = ?`,
		stateVersion,
	).Scan(&storedProvider, &storedModelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("unknown state version %d", stateVersion)
		}
		return fmt.Errorf("select staged state: %w", err)
	}
	if storedProvider != provider || storedModelID != modelID {
		return fmt.Errorf(
			"state version %d belongs to provider/model %q/%q, not %q/%q",
			stateVersion,
			storedProvider,
			storedModelID,
			provider,
			modelID,
		)
	}

	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO current_model_states(provider, model_id, state_version)
		 VALUES (?, ?, ?)
		 ON CONFLICT(provider, model_id) DO UPDATE SET state_version = excluded.state_version`,
		provider,
		modelID,
		stateVersion,
	); err != nil {
		return fmt.Errorf("activate state: %w", err)
	}

	return nil
}

func (s *Store) CleanupInactiveStates(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM file_hashes
		 WHERE state_version NOT IN (SELECT state_version FROM current_model_states)`,
	); err != nil {
		return fmt.Errorf("delete inactive file hashes: %w", err)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`DELETE FROM file_hash_states
		 WHERE state_version NOT IN (SELECT state_version FROM current_model_states)`,
	); err != nil {
		return fmt.Errorf("delete inactive states: %w", err)
	}
	return nil
}
