package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uchebnick/unch/internal/semsearch"
)

func defaultModelFlagValue() string {
	return ""
}

func previewStateTarget(rootAbs string, stateDirInput string, stateDirWasExplicit bool, dbInput string, dbWasExplicit bool) (semsearch.Paths, string, bool, error) {
	if stateDirWasExplicit && dbWasExplicit {
		return semsearch.Paths{}, "", false, fmt.Errorf("use either --state-dir or --db, not both")
	}
	if stateDirWasExplicit {
		return previewExplicitStateDirTarget(stateDirInput)
	}
	if dbWasExplicit {
		return previewLegacyIndexTarget(dbInput)
	}

	localDir := filepath.Join(rootAbs, ".semsearch")
	modelsDir, err := semsearch.DefaultModelsDir()
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	return semsearch.Paths{
		LocalDir:     localDir,
		ManifestPath: semsearch.ManifestFilePath(localDir),
		FileHashDB:   filepath.Join(localDir, "filehashes.db"),
		ModelsDir:    modelsDir,
	}, filepath.Join(localDir, "index.db"), true, nil
}

func resolveStateTarget(rootAbs string, stateDirInput string, stateDirWasExplicit bool, dbInput string, dbWasExplicit bool) (semsearch.Paths, string, bool, error) {
	preview, resolvedIndexPath, stateDirOwnsIndex, err := previewStateTarget(rootAbs, stateDirInput, stateDirWasExplicit, dbInput, dbWasExplicit)
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	paths, err := semsearch.PathsForLocalDir(preview.LocalDir)
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	return paths, resolvedIndexPath, stateDirOwnsIndex, nil
}

func previewExplicitStateDirTarget(stateDirInput string) (semsearch.Paths, string, bool, error) {
	resolvedStateDir, err := filepath.Abs(strings.TrimSpace(stateDirInput))
	if err != nil {
		return semsearch.Paths{}, "", false, fmt.Errorf("resolve state dir: %w", err)
	}

	modelsDir, err := semsearch.DefaultModelsDir()
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	paths := semsearch.Paths{
		LocalDir:     resolvedStateDir,
		ManifestPath: semsearch.ManifestFilePath(resolvedStateDir),
		FileHashDB:   filepath.Join(resolvedStateDir, "filehashes.db"),
		ModelsDir:    modelsDir,
	}
	return paths, filepath.Join(paths.LocalDir, "index.db"), true, nil
}

func previewLegacyIndexTarget(dbInput string) (semsearch.Paths, string, bool, error) {
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

	modelsDir, err := semsearch.DefaultModelsDir()
	if err != nil {
		return semsearch.Paths{}, "", false, err
	}
	paths := semsearch.Paths{
		LocalDir:     localDir,
		ManifestPath: semsearch.ManifestFilePath(localDir),
		FileHashDB:   filepath.Join(localDir, "filehashes.db"),
		ModelsDir:    modelsDir,
	}
	return paths, resolvedIndexPath, stateDirOwnsIndex, nil
}
