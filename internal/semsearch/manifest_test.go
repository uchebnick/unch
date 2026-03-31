package semsearch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultManifest(t *testing.T) {
	t.Parallel()

	got := DefaultManifest()
	if got.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("DefaultManifest().SchemaVersion = %d, want %d", got.SchemaVersion, ManifestSchemaVersion)
	}
	if got.Version != 0 {
		t.Fatalf("DefaultManifest().Version = %d, want 0", got.Version)
	}
	if got.IndexingHash != "" {
		t.Fatalf("DefaultManifest().IndexingHash = %q, want empty", got.IndexingHash)
	}
	if got.Source != "local" {
		t.Fatalf("DefaultManifest().Source = %q, want local", got.Source)
	}
	if got.Remote != nil {
		t.Fatalf("DefaultManifest().Remote = %+v, want nil", got.Remote)
	}
}

func TestWriteAndReadManifest(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	want := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       7,
		IndexingHash:  "abc123",
		Source:        "remote",
		Remote:        &Remote{CIURL: "https://github.com/org/repo/actions/workflows/index.yml"},
	}

	if err := WriteManifest(localDir, want); err != nil {
		t.Fatalf("WriteManifest() error: %v", err)
	}

	got, err := ReadManifest(localDir)
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadManifest() = %+v, want %+v", got, want)
	}
}

func TestEnsureManifest(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()

	manifest, created, err := EnsureManifest(localDir)
	if err != nil {
		t.Fatalf("EnsureManifest() error: %v", err)
	}
	if !created {
		t.Fatalf("expected manifest to be created")
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("manifest.SchemaVersion = %d", manifest.SchemaVersion)
	}
	if manifest.Version != 0 {
		t.Fatalf("manifest.Version = %d", manifest.Version)
	}
	if manifest.Source != "local" {
		t.Fatalf("manifest.Source = %q", manifest.Source)
	}

	path := ManifestFilePath(localDir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected manifest file at %s: %v", path, err)
	}

	manifest, created, err = EnsureManifest(localDir)
	if err != nil {
		t.Fatalf("EnsureManifest(second call) error: %v", err)
	}
	if created {
		t.Fatalf("expected second call not to recreate manifest")
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("manifest.SchemaVersion = %d", manifest.SchemaVersion)
	}
}

func TestManifestNormalize(t *testing.T) {
	t.Parallel()

	got := (Manifest{
		IndexingHash: "  abc123  ",
		Source:       "  REMOTE ",
		Remote:       &Remote{CIURL: "  https://example.test/workflow.yml  "},
	}).Normalize()

	if got.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("Normalize().SchemaVersion = %d", got.SchemaVersion)
	}
	if got.IndexingHash != "abc123" {
		t.Fatalf("Normalize().IndexingHash = %q", got.IndexingHash)
	}
	if got.Source != "remote" {
		t.Fatalf("Normalize().Source = %q", got.Source)
	}
	if got.Remote == nil || got.Remote.CIURL != "https://example.test/workflow.yml" {
		t.Fatalf("Normalize().Remote = %+v", got.Remote)
	}
}

func TestManifestValidateRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	err := (Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Source:        "weird",
	}).Validate()
	if err == nil {
		t.Fatalf("expected Validate() to reject unknown source")
	}
}

func TestManifestFilePath(t *testing.T) {
	t.Parallel()

	got := ManifestFilePath("/tmp/repo/.semsearch")
	want := filepath.Join("/tmp/repo/.semsearch", "manifest.json")
	if got != want {
		t.Fatalf("ManifestFilePath() = %q, want %q", got, want)
	}
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "index.db")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256() error: %v", err)
	}

	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("FileSHA256() = %q, want %q", got, want)
	}
}

func TestUpdateIndexManifest(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	dbPath := filepath.Join(localDir, "index.db")
	writeTestIndexDB(t, dbPath, 3, "/tmp/a.go", 10, "hash1", []float32{1, 2, 3})
	if _, err := BindRemoteManifest(localDir, "https://github.com/acme/widgets/actions/workflows/searcher.yml"); err != nil {
		t.Fatalf("BindRemoteManifest() error: %v", err)
	}

	manifest, err := UpdateIndexManifest(localDir, dbPath, 3)
	if err != nil {
		t.Fatalf("UpdateIndexManifest() error: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("manifest.Version = %d, want 1", manifest.Version)
	}
	if manifest.Source != "local" {
		t.Fatalf("manifest.Source = %q, want local", manifest.Source)
	}
	if manifest.Remote != nil {
		t.Fatalf("manifest.Remote = %+v, want nil after local indexing", manifest.Remote)
	}
	if manifest.IndexingHash == "" {
		t.Fatalf("expected non-empty indexing hash")
	}

	reloaded, err := ReadManifest(localDir)
	if err != nil {
		t.Fatalf("ReadManifest() error: %v", err)
	}
	if !reflect.DeepEqual(reloaded, manifest) {
		t.Fatalf("ReadManifest() = %+v, want %+v", reloaded, manifest)
	}

	manifest, err = UpdateIndexManifest(localDir, dbPath, 99)
	if err != nil {
		t.Fatalf("UpdateIndexManifest(second call) error: %v", err)
	}
	if manifest.Version != 2 {
		t.Fatalf("manifest.Version after second update = %d, want 2", manifest.Version)
	}
}
