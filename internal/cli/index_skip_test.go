package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uchebnick/unch/internal/filehashdb"
	"github.com/uchebnick/unch/internal/semsearch"
)

func TestMaybeSkipUnchangedIndexSkipsMatchingLocalState(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	dbPath := filepath.Join(localDir, "index.db")
	if err := writeStubFile(dbPath); err != nil {
		t.Fatalf("write index db: %v", err)
	}

	store, err := filehashdb.Open(context.Background(), filepath.Join(localDir, "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	files := map[string]string{"internal/indexing/scanner.go": "abc123"}
	version, err := store.StageState(context.Background(), "embeddinggemma", "scan-v1", files)
	if err != nil {
		t.Fatalf("StageState() error: %v", err)
	}
	if err := store.ActivateState(context.Background(), "embeddinggemma", version); err != nil {
		t.Fatalf("ActivateState() error: %v", err)
	}

	skipped, err := maybeSkipUnchangedIndex(
		context.Background(),
		semsearch.Paths{LocalDir: localDir},
		dbPath,
		semsearch.Manifest{Version: 7, Source: "local"},
		store,
		"embeddinggemma",
		"scan-v1",
		files,
		true,
		nil,
	)
	if err != nil {
		t.Fatalf("maybeSkipUnchangedIndex() error: %v", err)
	}
	if !skipped {
		t.Fatalf("maybeSkipUnchangedIndex() = false, want true")
	}
}

func TestMaybeSkipUnchangedIndexDoesNotSkipMismatchedState(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	dbPath := filepath.Join(localDir, "index.db")
	if err := writeStubFile(dbPath); err != nil {
		t.Fatalf("write index db: %v", err)
	}

	store, err := filehashdb.Open(context.Background(), filepath.Join(localDir, "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	version, err := store.StageState(context.Background(), "embeddinggemma", "scan-v1", map[string]string{"a.go": "aaa"})
	if err != nil {
		t.Fatalf("StageState() error: %v", err)
	}
	if err := store.ActivateState(context.Background(), "embeddinggemma", version); err != nil {
		t.Fatalf("ActivateState() error: %v", err)
	}

	skipped, err := maybeSkipUnchangedIndex(
		context.Background(),
		semsearch.Paths{LocalDir: localDir},
		dbPath,
		semsearch.Manifest{Version: 7, Source: "local"},
		store,
		"embeddinggemma",
		"scan-v1",
		map[string]string{"a.go": "bbb"},
		true,
		nil,
	)
	if err != nil {
		t.Fatalf("maybeSkipUnchangedIndex() error: %v", err)
	}
	if skipped {
		t.Fatalf("maybeSkipUnchangedIndex() = true, want false")
	}
}

func TestMaybeSkipUnchangedIndexDoesNotSkipExplicitOrRemoteDB(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	defaultDBPath := filepath.Join(localDir, "index.db")
	if err := writeStubFile(defaultDBPath); err != nil {
		t.Fatalf("write index db: %v", err)
	}

	store, err := filehashdb.Open(context.Background(), filepath.Join(localDir, "filehashes.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	files := map[string]string{"a.go": "aaa"}
	version, err := store.StageState(context.Background(), "embeddinggemma", "scan-v1", files)
	if err != nil {
		t.Fatalf("StageState() error: %v", err)
	}
	if err := store.ActivateState(context.Background(), "embeddinggemma", version); err != nil {
		t.Fatalf("ActivateState() error: %v", err)
	}

	cases := []struct {
		name           string
		resolvedDB     string
		stateDirOwnsDB bool
		manifest       semsearch.Manifest
	}{
		{
			name:           "legacy custom db path",
			resolvedDB:     filepath.Join(localDir, "custom.db"),
			stateDirOwnsDB: false,
			manifest:       semsearch.Manifest{Version: 7, Source: "local"},
		},
		{
			name:           "remote binding",
			resolvedDB:     defaultDBPath,
			stateDirOwnsDB: true,
			manifest: semsearch.Manifest{
				Version: 7,
				Source:  "remote",
				Remote:  &semsearch.Remote{CIURL: "https://github.com/acme/widgets/actions/workflows/searcher.yml"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.resolvedDB != defaultDBPath {
				if err := writeStubFile(tc.resolvedDB); err != nil {
					t.Fatalf("write custom db: %v", err)
				}
			}
			skipped, err := maybeSkipUnchangedIndex(
				context.Background(),
				semsearch.Paths{LocalDir: localDir},
				tc.resolvedDB,
				tc.manifest,
				store,
				"embeddinggemma",
				"scan-v1",
				files,
				tc.stateDirOwnsDB,
				nil,
			)
			if err != nil {
				t.Fatalf("maybeSkipUnchangedIndex() error: %v", err)
			}
			if skipped {
				t.Fatalf("maybeSkipUnchangedIndex() = true, want false")
			}
		})
	}
}

func writeStubFile(path string) error {
	return os.WriteFile(path, []byte("stub"), 0o644)
}
