package semsearch

import (
	"fmt"
	"os"
	"path/filepath"
)

const DefaultGitignore = "*\n"

// Init ensures the local .semsearch workspace exists and contains its baseline files.
func Init(root string) (Paths, bool, error) {
	paths, err := PreparePaths(root)
	if err != nil {
		return Paths{}, false, err
	}

	created, err := EnsureGitignore(paths.LocalDir)
	if err != nil {
		return Paths{}, false, err
	}
	if _, _, err := EnsureManifest(paths.LocalDir); err != nil {
		return Paths{}, false, err
	}

	return paths, created, nil
}

// EnsureGitignore creates the local .semsearch/.gitignore when it does not exist yet.
func EnsureGitignore(localDir string) (bool, error) {
	path := filepath.Join(localDir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(DefaultGitignore), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
