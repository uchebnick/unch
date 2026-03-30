package indexdb

import (
	"context"
	"path/filepath"
	"testing"
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
	if err := store.UpsertComment(ctx, "/tmp/a.go", 10, "hash1", 1); err != nil {
		t.Fatalf("UpsertComment(hash1) error: %v", err)
	}
	if err := store.UpsertComment(ctx, "/tmp/b.go", 20, "hash2", 0); err != nil {
		t.Fatalf("UpsertComment(hash2) error: %v", err)
	}
	if err := store.ActivateVersion(ctx, 1); err != nil {
		t.Fatalf("ActivateVersion() error: %v", err)
	}

	exists, err := store.EmbeddingExists(ctx, "hash1")
	if err != nil || !exists {
		t.Fatalf("EmbeddingExists(hash1) = (%v, %v)", exists, err)
	}

	listed, err := store.ListCurrentComments(ctx)
	if err != nil {
		t.Fatalf("ListCurrentComments() error: %v", err)
	}
	if len(listed) != 1 || listed[0].Path != "/tmp/a.go" {
		t.Fatalf("ListCurrentComments() = %+v", listed)
	}

	results, err := store.SearchCurrent(ctx, vec, 5)
	if err != nil {
		t.Fatalf("SearchCurrent() error: %v", err)
	}
	if len(results) == 0 || results[0].Path != "/tmp/a.go" {
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
