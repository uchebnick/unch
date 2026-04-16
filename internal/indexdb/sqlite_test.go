package indexdb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/uchebnick/unch/internal/indexing"
)

const testProvider = "llama.cpp"

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	const modelID = "embeddinggemma"

	snapshot1, err := store.BeginSnapshot(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot() error: %v", err)
	}
	snapshot2, err := store.BeginSnapshot(ctx, testProvider, "qwen3")
	if err != nil {
		t.Fatalf("BeginSnapshot(qwen3) error: %v", err)
	}

	vec := []float32{1, 0, 0}
	if err := store.AddEmbedding(ctx, testProvider, modelID, "hash1", vec); err != nil {
		t.Fatalf("AddEmbedding(hash1) error: %v", err)
	}
	if err := store.AddEmbedding(ctx, testProvider, "qwen3", "hash2", []float32{0, 1, 0}); err != nil {
		t.Fatalf("AddEmbedding(hash2) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, snapshot1, testProvider, modelID, "a.go", indexing.IndexedSymbol{
		Line:          10,
		Kind:          "function",
		Name:          "A",
		QualifiedName: "A",
		Signature:     "func A()",
		Documentation: "A docs",
	}, "hash1"); err != nil {
		t.Fatalf("InsertSymbol(hash1) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, snapshot2, testProvider, "qwen3", "b.go", indexing.IndexedSymbol{
		Line:          20,
		Kind:          "function",
		Name:          "B",
		QualifiedName: "B",
		Signature:     "func B()",
		Documentation: "B docs",
	}, "hash2"); err != nil {
		t.Fatalf("InsertSymbol(hash2) error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, testProvider, modelID, snapshot1); err != nil {
		t.Fatalf("ActivateSnapshot() error: %v", err)
	}

	currentSnapshot, err := store.CurrentSnapshot(ctx, testProvider, modelID)
	if err != nil || currentSnapshot != snapshot1 {
		t.Fatalf("CurrentSnapshot() = (%d, %v), want (%d, nil)", currentSnapshot, err, snapshot1)
	}

	exists, err := store.EmbeddingExists(ctx, testProvider, modelID, "hash1")
	if err != nil || !exists {
		t.Fatalf("EmbeddingExists(hash1) = (%v, %v)", exists, err)
	}

	listed, err := store.ListCurrentSymbols(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("ListCurrentSymbols() error: %v", err)
	}
	if len(listed) != 1 || listed[0].Path != "a.go" || listed[0].QualifiedName != "A" {
		t.Fatalf("ListCurrentSymbols() = %+v", listed)
	}

	results, err := store.SearchCurrent(ctx, testProvider, modelID, vec, 5)
	if err != nil {
		t.Fatalf("SearchCurrent() error: %v", err)
	}
	if len(results) == 0 || results[0].Path != "a.go" || results[0].QualifiedName != "A" {
		t.Fatalf("SearchCurrent() = %+v", results)
	}

	if _, err := store.ListCurrentSymbols(ctx, testProvider, "qwen3"); !errors.Is(err, ErrNoActiveSnapshot) {
		t.Fatalf("ListCurrentSymbols(qwen3) error = %v, want ErrNoActiveSnapshot", err)
	}

	if err := store.CleanupInactiveSnapshots(ctx); err != nil {
		t.Fatalf("CleanupInactiveSnapshots() error: %v", err)
	}
	if err := store.CleanupUnusedEmbeddings(ctx); err != nil {
		t.Fatalf("CleanupUnusedEmbeddings() error: %v", err)
	}
	exists, err = store.EmbeddingExists(ctx, testProvider, "qwen3", "hash2")
	if err != nil {
		t.Fatalf("EmbeddingExists(hash2) error: %v", err)
	}
	if exists {
		t.Fatalf("expected qwen3/hash2 embedding to be removed after cleanup")
	}
}

func TestActiveSnapshotIsolationAcrossModels(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	if err := store.AddEmbedding(ctx, testProvider, "embeddinggemma", "hash-gemma", []float32{1, 0, 0}); err != nil {
		t.Fatalf("AddEmbedding(gemma) error: %v", err)
	}
	gemmaSnapshot, err := store.BeginSnapshot(ctx, testProvider, "embeddinggemma")
	if err != nil {
		t.Fatalf("BeginSnapshot(gemma) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, gemmaSnapshot, testProvider, "embeddinggemma", "a.go", indexing.IndexedSymbol{
		Line:          10,
		Kind:          "function",
		Name:          "A",
		QualifiedName: "A",
	}, "hash-gemma"); err != nil {
		t.Fatalf("InsertSymbol(gemma) error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, testProvider, "embeddinggemma", gemmaSnapshot); err != nil {
		t.Fatalf("ActivateSnapshot(gemma) error: %v", err)
	}

	qwenSnapshot, err := store.BeginSnapshot(ctx, testProvider, "qwen3")
	if err != nil {
		t.Fatalf("BeginSnapshot(qwen3) error: %v", err)
	}
	if err := store.AddEmbedding(ctx, testProvider, "qwen3", "hash-qwen", []float32{0, 1, 0}); err != nil {
		t.Fatalf("AddEmbedding(qwen3) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, qwenSnapshot, testProvider, "qwen3", "b.go", indexing.IndexedSymbol{
		Line:          20,
		Kind:          "function",
		Name:          "B",
		QualifiedName: "B",
	}, "hash-qwen"); err != nil {
		t.Fatalf("InsertSymbol(qwen3) error: %v", err)
	}

	listed, err := store.ListCurrentSymbols(ctx, testProvider, "embeddinggemma")
	if err != nil {
		t.Fatalf("ListCurrentSymbols(gemma) error: %v", err)
	}
	if len(listed) != 1 || listed[0].Path != "a.go" {
		t.Fatalf("ListCurrentSymbols(gemma) = %+v", listed)
	}
}

func TestLogicalHashIgnoresSnapshotOnlyChanges(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	modelID := "embeddinggemma"
	vec := []float32{1, 0, 0}
	if err := store.AddEmbedding(ctx, testProvider, modelID, "hash1", vec); err != nil {
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

	firstSnapshot, err := store.BeginSnapshot(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot(first) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, firstSnapshot, testProvider, modelID, "a.go", symbol, "hash1"); err != nil {
		t.Fatalf("InsertSymbol(first) error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, testProvider, modelID, firstSnapshot); err != nil {
		t.Fatalf("ActivateSnapshot(first) error: %v", err)
	}

	firstHash, err := LogicalHash(ctx, dbPath)
	if err != nil {
		t.Fatalf("LogicalHash(first) error: %v", err)
	}

	secondSnapshot, err := store.BeginSnapshot(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot(second) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, secondSnapshot, testProvider, modelID, "a.go", symbol, "hash1"); err != nil {
		t.Fatalf("InsertSymbol(second) error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, testProvider, modelID, secondSnapshot); err != nil {
		t.Fatalf("ActivateSnapshot(second) error: %v", err)
	}

	secondHash, err := LogicalHash(ctx, dbPath)
	if err != nil {
		t.Fatalf("LogicalHash(second) error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("LogicalHash() changed after snapshot-only update: first=%s second=%s", firstHash, secondHash)
	}
}

func TestLogicalHashRejectsLegacySchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	defer func() {
		_ = db.Close()
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

func TestCopyPathFromSnapshot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")

	store, err := Open(ctx, dbPath, 3)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	modelID := "embeddinggemma"
	if err := store.AddEmbedding(ctx, testProvider, modelID, "hash-a", []float32{1, 0, 0}); err != nil {
		t.Fatalf("AddEmbedding() error: %v", err)
	}

	srcSnapshot, err := store.BeginSnapshot(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot(src) error: %v", err)
	}
	if err := store.InsertSymbol(ctx, srcSnapshot, testProvider, modelID, "a.go", indexing.IndexedSymbol{
		Line:          10,
		Kind:          "function",
		Name:          "A",
		QualifiedName: "A",
	}, "hash-a"); err != nil {
		t.Fatalf("InsertSymbol(src) error: %v", err)
	}
	if err := store.ActivateSnapshot(ctx, testProvider, modelID, srcSnapshot); err != nil {
		t.Fatalf("ActivateSnapshot(src) error: %v", err)
	}

	dstSnapshot, err := store.BeginSnapshot(ctx, testProvider, modelID)
	if err != nil {
		t.Fatalf("BeginSnapshot(dst) error: %v", err)
	}
	copied, err := store.CopyPathFromSnapshot(ctx, testProvider, modelID, srcSnapshot, dstSnapshot, "a.go")
	if err != nil {
		t.Fatalf("CopyPathFromSnapshot() error: %v", err)
	}
	if copied != 1 {
		t.Fatalf("CopyPathFromSnapshot() copied = %d, want 1", copied)
	}

	listed, err := store.ListSymbolsBySnapshot(ctx, testProvider, modelID, dstSnapshot)
	if err != nil {
		t.Fatalf("ListSymbolsBySnapshot(dst) error: %v", err)
	}
	if len(listed) != 1 || listed[0].Path != "a.go" || listed[0].QualifiedName != "A" {
		t.Fatalf("ListSymbolsBySnapshot(dst) = %+v", listed)
	}
}
