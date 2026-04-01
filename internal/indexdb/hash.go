package indexdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

var ErrIncompatibleSchema = errors.New("incompatible index schema")

// LogicalHash computes a stable hash of the active logical index contents,
// ignoring SQLite file layout and other storage-level noise.
func LogicalHash(ctx context.Context, dbPath string) (string, error) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := ensureLogicalHashSchema(ctx, db); err != nil {
		return "", err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT
			cs.model_id,
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
			s.embedding_hash,
			e.embedding
		FROM current_model_snapshots cs
		JOIN snapshot_symbols s
		  ON s.model_id = cs.model_id
		 AND s.snapshot_id = cs.snapshot_id
		JOIN snapshot_embeddings e
		  ON e.model_id = s.model_id
		 AND e.embedding_hash = s.embedding_hash
		ORDER BY cs.model_id ASC, s.path ASC, s.line ASC, s.symbol_id ASC`,
	)
	if err != nil {
		if isSchemaQueryError(err) {
			return "", fmt.Errorf("%w: %v", ErrIncompatibleSchema, err)
		}
		return "", fmt.Errorf("query logical hash rows: %w", err)
	}
	defer rows.Close()

	sum := sha256.New()
	writeHashBytes(sum, []byte("semsearch-logical-index-v2"))

	for rows.Next() {
		var modelID string
		var path string
		var line int64
		var symbolID string
		var kind string
		var name string
		var container string
		var qualifiedName string
		var signature string
		var documentation string
		var body string
		var embeddingHash string
		var embedding []byte
		if err := rows.Scan(
			&modelID,
			&path,
			&line,
			&symbolID,
			&kind,
			&name,
			&container,
			&qualifiedName,
			&signature,
			&documentation,
			&body,
			&embeddingHash,
			&embedding,
		); err != nil {
			return "", fmt.Errorf("scan logical hash row: %w", err)
		}

		writeHashString(sum, modelID)
		writeHashString(sum, path)
		writeHashInt64(sum, line)
		writeHashString(sum, symbolID)
		writeHashString(sum, kind)
		writeHashString(sum, name)
		writeHashString(sum, container)
		writeHashString(sum, qualifiedName)
		writeHashString(sum, signature)
		writeHashString(sum, documentation)
		writeHashString(sum, body)
		writeHashString(sum, embeddingHash)
		writeHashBytes(sum, embedding)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate logical hash rows: %w", err)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

func ensureLogicalHashSchema(ctx context.Context, db *sql.DB) error {
	requiredTables := []string{"snapshot_symbols", "snapshot_embeddings", "current_model_snapshots"}
	rows, err := db.QueryContext(
		ctx,
		`SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name IN ('snapshot_symbols', 'snapshot_embeddings', 'current_model_snapshots')
		ORDER BY name ASC`,
	)
	if err != nil {
		return fmt.Errorf("inspect logical hash schema: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool, len(requiredTables))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan logical hash schema: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate logical hash schema: %w", err)
	}

	var missing []string
	for _, name := range requiredTables {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing tables %s", ErrIncompatibleSchema, strings.Join(missing, ", "))
	}

	return nil
}

func isSchemaQueryError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "no such column")
}

func writeHashString(sum hash.Hash, value string) {
	writeHashBytes(sum, []byte(value))
}

func writeHashBytes(sum hash.Hash, value []byte) {
	writeHashInt64(sum, int64(len(value)))
	_, _ = sum.Write(value)
}

func writeHashInt64(sum hash.Hash, value int64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(value))
	_, _ = sum.Write(buf[:])
}
