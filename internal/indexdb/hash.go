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
			c.path,
			c.line,
			c.comment_hash,
			e.embedding
		FROM comments c
		JOIN embeddings e ON e.comment_hash = c.comment_hash
		WHERE c.version = (SELECT value FROM meta WHERE key = 'current_version')
		ORDER BY c.path ASC, c.line ASC, c.comment_hash ASC`,
	)
	if err != nil {
		return "", fmt.Errorf("query logical hash rows: %w", err)
	}
	defer rows.Close()

	sum := sha256.New()
	writeHashBytes(sum, []byte("semsearch-logical-index-v1"))

	for rows.Next() {
		var path string
		var line int64
		var commentHash string
		var embedding []byte
		if err := rows.Scan(&path, &line, &commentHash, &embedding); err != nil {
			return "", fmt.Errorf("scan logical hash row: %w", err)
		}

		writeHashString(sum, path)
		writeHashInt64(sum, line)
		writeHashString(sum, commentHash)
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
