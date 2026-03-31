package semsearch

import (
	"context"
	"os"
	"testing"

	"github.com/uchebnick/unch-searcher/internal/indexdb"
)

func writeTestIndexDB(t *testing.T, dbPath string, version int64, path string, line int, commentHash string, embedding []float32) string {
	t.Helper()

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

	if err := store.AddEmbedding(ctx, commentHash, embedding); err != nil {
		t.Fatalf("AddEmbedding() error: %v", err)
	}
	if err := store.UpsertComment(ctx, path, line, commentHash, version); err != nil {
		t.Fatalf("UpsertComment() error: %v", err)
	}
	if err := store.ActivateVersion(ctx, version); err != nil {
		t.Fatalf("ActivateVersion() error: %v", err)
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
