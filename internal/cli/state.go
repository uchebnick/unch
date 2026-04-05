package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/runtime"
	"github.com/uchebnick/unch/internal/semsearch"
)

func defaultModelFlagValue() string {
	modelsDir, err := semsearch.DefaultModelsDir()
	if err != nil {
		return ""
	}
	return runtime.DefaultModelPath(modelsDir)
}

func resolveStateTarget(rootAbs string, stateDirInput string, stateDirWasExplicit bool, dbInput string, dbWasExplicit bool) (semsearch.Paths, string, bool, error) {
	if stateDirWasExplicit && dbWasExplicit {
		return semsearch.Paths{}, "", false, fmt.Errorf("use either --state-dir or --db, not both")
	}
	if stateDirWasExplicit {
		return resolveExplicitStateDirTarget(stateDirInput)
	}
	if dbWasExplicit {
		return resolveLegacyIndexTarget(dbInput)
	}

	paths, err := semsearch.PreparePaths(rootAbs)
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	return paths, filepath.Join(paths.LocalDir, "index.db"), true, nil
}

func resolveExplicitStateDirTarget(stateDirInput string) (semsearch.Paths, string, bool, error) {
	resolvedStateDir, err := filepath.Abs(strings.TrimSpace(stateDirInput))
	if err != nil {
		return semsearch.Paths{}, "", false, fmt.Errorf("resolve state dir: %w", err)
	}

	paths, err := semsearch.PathsForLocalDir(resolvedStateDir)
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	return paths, filepath.Join(paths.LocalDir, "index.db"), true, nil
}

func resolveLegacyIndexTarget(dbInput string) (semsearch.Paths, string, bool, error) {
	resolvedInput, err := filepath.Abs(strings.TrimSpace(dbInput))
	if err != nil {
		return semsearch.Paths{}, "", false, fmt.Errorf("resolve legacy index path: %w", err)
	}

	info, statErr := os.Stat(resolvedInput)
	localDir := ""
	resolvedIndexPath := resolvedInput
	stateDirOwnsIndex := false

	switch {
	case statErr == nil && info.IsDir():
		localDir = resolvedInput
		resolvedIndexPath = filepath.Join(localDir, "index.db")
		stateDirOwnsIndex = true
	case statErr == nil:
		localDir = filepath.Dir(resolvedInput)
		stateDirOwnsIndex = strings.EqualFold(filepath.Base(resolvedInput), "index.db")
	case os.IsNotExist(statErr) && strings.EqualFold(filepath.Base(resolvedInput), ".semsearch"):
		localDir = resolvedInput
		resolvedIndexPath = filepath.Join(localDir, "index.db")
		stateDirOwnsIndex = true
	case os.IsNotExist(statErr):
		localDir = filepath.Dir(resolvedInput)
		stateDirOwnsIndex = strings.EqualFold(filepath.Base(resolvedInput), "index.db")
	default:
		return semsearch.Paths{}, "", false, fmt.Errorf("stat legacy index path: %w", statErr)
	}

	paths, err := semsearch.PathsForLocalDir(localDir)
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	return paths, resolvedIndexPath, stateDirOwnsIndex, nil
}
