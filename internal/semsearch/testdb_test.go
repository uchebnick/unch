package semsearch

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/uchebnick/unch/internal/indexdb"
	"github.com/uchebnick/unch/internal/indexing"
)

func writeTestIndexDB(t *testing.T, dbPath string, version int64, path string, line int, commentHash string, embedding []float32) string {
	t.Helper()
	_ = version

	ctx := context.Background()
	store, err := indexdb.Open(ctx, dbPath, len(embedding))
	if err != nil {
		t.Fatalf("indexdb.Open() error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error: %v", err)
		}
	}()

	const modelID = "embeddinggemma"

	snapshotID, err := store.BeginSnapshot(ctx, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot() error: %v", err)
	}
	if err := store.AddEmbedding(ctx, modelID, commentHash, embedding); err != nil {
		t.Fatalf("AddEmbedding() error: %v", err)
	}
	if err := store.InsertSymbol(ctx, snapshotID, modelID, path, indexing.IndexedSymbol{
		Line:          line,
		Kind:          "function",
		Name:          "TestSymbol",
		QualifiedName: "TestSymbol",
		Signature:     "func TestSymbol()",
		Documentation: "test symbol",
	}, commentHash); err != nil {
		t.Fatalf("InsertSymbol() error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, modelID, snapshotID); err != nil {
		t.Fatalf("ActivateSnapshot() error: %v", err)
	}

	gotHash, err := indexdb.LogicalHash(ctx, dbPath)
	if err != nil {
		t.Fatalf("LogicalHash() error: %v", err)
	}
	return gotHash
}

func readTestIndexDBBytes(t *testing.T, dbPath string) []byte {
	t.Helper()

	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error: %v", dbPath, err)
	}
	return data
}

func writeLegacyTestIndexDB(t *testing.T, dbPath string, version int64) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error: %v", err)
		}
	}()

	for _, stmt := range []string{
		`CREATE TABLE comments (
			path TEXT NOT NULL,
			line INTEGER NOT NULL,
			comment_hash TEXT NOT NULL,
			version INTEGER NOT NULL,
			PRIMARY KEY (path, line)
		);`,
		`CREATE TABLE embeddings (
			comment_hash TEXT PRIMARY KEY,
			embedding BLOB NOT NULL
		);`,
		`CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		);`,
		`INSERT INTO meta(key, value) VALUES ('current_version', ?);`,
		`INSERT INTO embeddings(comment_hash, embedding) VALUES ('legacy-hash', X'000000');`,
		`INSERT INTO comments(path, line, comment_hash, version) VALUES ('legacy.go', 10, 'legacy-hash', ?);`,
	} {
		switch stmt {
		case `INSERT INTO meta(key, value) VALUES ('current_version', ?);`,
			`INSERT INTO comments(path, line, comment_hash, version) VALUES ('legacy.go', 10, 'legacy-hash', ?);`:
			if _, err := db.Exec(stmt, version); err != nil {
				t.Fatalf("db.Exec(%q) error: %v", stmt, err)
			}
		default:
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("db.Exec(%q) error: %v", stmt, err)
			}
		}
	}
}
