package indexdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// LogicalHash computes a stable hash of the active logical index contents,
// ignoring SQLite file layout and other storage-level noise.
func LogicalHash(ctx context.Context, dbPath string) (string, error) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(
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
			s.embedding_hash,
			e.embedding
		FROM symbols s
		JOIN embeddings e ON e.comment_hash = s.embedding_hash
		WHERE s.version = (SELECT value FROM meta WHERE key = 'current_version')
		ORDER BY s.path ASC, s.line ASC, s.symbol_id ASC`,
	)
	if err != nil {
		return "", fmt.Errorf("query logical hash rows: %w", err)
	}
	defer rows.Close()

	sum := sha256.New()
	writeHashBytes(sum, []byte("semsearch-logical-index-v2"))

	for rows.Next() {
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
