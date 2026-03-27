package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
