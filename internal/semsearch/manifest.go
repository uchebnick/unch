package semsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch-searcher/internal/indexdb"
)

const ManifestSchemaVersion = 1

type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	Version       int64   `json:"version"`
	IndexingHash  string  `json:"indexing_hash"`
	Source        string  `json:"source"`
	Remote        *Remote `json:"remote,omitempty"`
}

type Remote struct {
	CIURL string `json:"ci_url,omitempty"`
}

func ManifestFilePath(localDir string) string {
	return filepath.Join(localDir, "manifest.json")
}

func DefaultManifest() Manifest {
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Version:       0,
		IndexingHash:  "",
		Source:        "local",
	}
}

func (m Manifest) Normalize() Manifest {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = ManifestSchemaVersion
	}
	m.Version = max(m.Version, 0)
	m.IndexingHash = strings.TrimSpace(m.IndexingHash)
	m.Source = strings.TrimSpace(strings.ToLower(m.Source))
	if m.Remote != nil {
		normalized := *m.Remote
		normalized.CIURL = strings.TrimSpace(normalized.CIURL)
		if normalized.CIURL == "" {
			m.Remote = nil
		} else {
			m.Remote = &normalized
		}
	}
	return m
}

func (m Manifest) Validate() error {
	if m.SchemaVersion <= 0 {
		return fmt.Errorf("invalid manifest schema_version %d", m.SchemaVersion)
	}
	switch m.Source {
	case "", "local", "remote":
	default:
		return fmt.Errorf("invalid manifest source %q", m.Source)
	}
	return nil
}

func ReadManifest(localDir string) (Manifest, error) {
	path := ManifestFilePath(localDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode %s: %w", path, err)
	}

	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return manifest, nil
}

func WriteManifest(localDir string, manifest Manifest) error {
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", localDir, err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')

	path := ManifestFilePath(localDir)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func EnsureManifest(localDir string) (Manifest, bool, error) {
	path := ManifestFilePath(localDir)
	if _, err := os.Stat(path); err == nil {
		manifest, err := ReadManifest(localDir)
		return manifest, false, err
	} else if !os.IsNotExist(err) {
		return Manifest{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	manifest := DefaultManifest()
	if err := WriteManifest(localDir, manifest); err != nil {
		return Manifest{}, false, err
	}
	return manifest, true, nil
}

func UpdateIndexManifest(localDir string, dbPath string, _ int64) (Manifest, error) {
	manifest, _, err := EnsureManifest(localDir)
	if err != nil {
		return Manifest{}, err
	}

	indexingHash, err := indexdb.LogicalHash(context.Background(), dbPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("hash %s: %w", dbPath, err)
	}

	manifest.Version++
	manifest.IndexingHash = indexingHash
	manifest.Source = "local"
	manifest.Remote = nil

	if err := WriteManifest(localDir, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
