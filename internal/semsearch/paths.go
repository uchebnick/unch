package semsearch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths contains the repository-local and shared filesystem locations used by unch.
type Paths struct {
	LocalDir     string
	ManifestPath string
	FileHashDB   string
	ModelsDir    string
}

// DefaultModelsDir resolves the shared model cache directory used by unch.
func DefaultModelsDir() (string, error) {
	globalDir, err := globalSemsearchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(globalDir, "models"), nil
}

// PathsForLocalDir creates the local and global directories used by unch for
// repository state, manifests, and shared model storage.
func PathsForLocalDir(localDir string) (Paths, error) {
	modelsDir, err := DefaultModelsDir()
	if err != nil {
		return Paths{}, err
	}

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create local dir: %w", err)
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create global models dir: %w", err)
	}

	return Paths{
		LocalDir:     localDir,
		ManifestPath: ManifestFilePath(localDir),
		FileHashDB:   filepath.Join(localDir, "filehashes.db"),
		ModelsDir:    modelsDir,
	}, nil
}

// PreparePaths creates the local and global directories used by unch for
// repository state, manifests, and shared model storage.
func PreparePaths(root string) (Paths, error) {
	return PathsForLocalDir(filepath.Join(root, ".semsearch"))
}

func globalSemsearchDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("SEMSEARCH_HOME")); custom != "" {
		return filepath.Abs(custom)
	}

	cacheDir, err := os.UserCacheDir()
	if err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "unch"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("resolve global semsearch dir: %w", err)
	}

	return filepath.Join(homeDir, ".semsearch"), nil
}
