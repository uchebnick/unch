package indexdb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/uchebnick/unch/internal/indexing"
)

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer store.Close()

	current, err := store.CurrentVersion(ctx)
	if err != nil || current != 0 {
		t.Fatalf("CurrentVersion() = (%d, %v)", current, err)
	}

	working, err := store.WorkingVersion(ctx)
	if err != nil || working != 1 {
		t.Fatalf("WorkingVersion() = (%d, %v)", working, err)
	}

	vec := []float32{1, 0, 0}
	if err := store.AddEmbedding(ctx, "hash1", vec); err != nil {
		t.Fatalf("AddEmbedding(hash1) error: %v", err)
	}
	if err := store.AddEmbedding(ctx, "hash2", []float32{0, 1, 0}); err != nil {
		t.Fatalf("AddEmbedding(hash2) error: %v", err)
	}
	if err := store.UpsertSymbol(ctx, "a.go", indexing.IndexedSymbol{
		Line:          10,
		Kind:          "function",
		Name:          "A",
		QualifiedName: "A",
		Signature:     "func A()",
		Documentation: "A docs",
	}, "hash1", 1); err != nil {
		t.Fatalf("UpsertSymbol(hash1) error: %v", err)
	}
	if err := store.UpsertSymbol(ctx, "b.go", indexing.IndexedSymbol{
		Line:          20,
		Kind:          "function",
		Name:          "B",
		QualifiedName: "B",
		Signature:     "func B()",
		Documentation: "B docs",
	}, "hash2", 0); err != nil {
		t.Fatalf("UpsertSymbol(hash2) error: %v", err)
	}
	if err := store.ActivateVersion(ctx, 1); err != nil {
		t.Fatalf("ActivateVersion() error: %v", err)
	}

	exists, err := store.EmbeddingExists(ctx, "hash1")
	if err != nil || !exists {
		t.Fatalf("EmbeddingExists(hash1) = (%v, %v)", exists, err)
	}

	listed, err := store.ListCurrentSymbols(ctx)
	if err != nil {
		t.Fatalf("ListCurrentSymbols() error: %v", err)
	}
	if len(listed) != 1 || listed[0].Path != "a.go" || listed[0].QualifiedName != "A" {
		t.Fatalf("ListCurrentSymbols() = %+v", listed)
	}

	results, err := store.SearchCurrent(ctx, vec, 5)
	if err != nil {
		t.Fatalf("SearchCurrent() error: %v", err)
	}
	if len(results) == 0 || results[0].Path != "a.go" || results[0].QualifiedName != "A" {
		t.Fatalf("SearchCurrent() = %+v", results)
	}

	if err := store.CleanupOldVersions(ctx, 1); err != nil {
		t.Fatalf("CleanupOldVersions() error: %v", err)
	}
	if err := store.CleanupUnusedEmbeddings(ctx); err != nil {
		t.Fatalf("CleanupUnusedEmbeddings() error: %v", err)
	}
	exists, err = store.EmbeddingExists(ctx, "hash2")
	if err != nil {
		t.Fatalf("EmbeddingExists(hash2) error: %v", err)
	}
	if exists {
		t.Fatalf("expected hash2 embedding to be removed after cleanup")
	}
}

func TestLogicalHashIgnoresCurrentVersionMetadata(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer store.Close()

	vec := []float32{1, 0, 0}
	if err := store.AddEmbedding(ctx, "hash1", vec); err != nil {
		t.Fatalf("AddEmbedding() error: %v", err)
	}
	symbol := indexing.IndexedSymbol{
		Line:          10,
		Kind:          "function",
		Name:          "A",
		QualifiedName: "A",
		Signature:     "func A()",
		Documentation: "A docs",
	}
	if err := store.UpsertSymbol(ctx, "a.go", symbol, "hash1", 1); err != nil {
		t.Fatalf("UpsertSymbol(version=1) error: %v", err)
	}
	if err := store.ActivateVersion(ctx, 1); err != nil {
		t.Fatalf("ActivateVersion(1) error: %v", err)
	}

	firstHash, err := LogicalHash(ctx, dbPath)
	if err != nil {
		t.Fatalf("LogicalHash(first) error: %v", err)
	}

	if err := store.UpsertSymbol(ctx, "a.go", symbol, "hash1", 2); err != nil {
		t.Fatalf("UpsertSymbol(version=2) error: %v", err)
	}
	if err := store.ActivateVersion(ctx, 2); err != nil {
		t.Fatalf("ActivateVersion(2) error: %v", err)
	}

	secondHash, err := LogicalHash(ctx, dbPath)
	if err != nil {
		t.Fatalf("LogicalHash(second) error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("LogicalHash() changed after version-only update: first=%s second=%s", firstHash, secondHash)
	}
}

func TestLogicalHashRejectsLegacySchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	defer db.Close()

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
		`INSERT INTO meta(key, value) VALUES ('current_version', 1);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("db.ExecContext(%q) error: %v", stmt, err)
		}
	}

	_, err = LogicalHash(ctx, dbPath)
	if err == nil || !errors.Is(err, ErrIncompatibleSchema) {
		t.Fatalf("LogicalHash() error = %v, want ErrIncompatibleSchema", err)
	}
}
