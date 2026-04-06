package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uchebnick/unch/internal/modelcatalog"
)

type Reporter interface {
	Logf(format string, args ...any)
	ProgressTracker(label string) ProgressTracker
}

type ModelCache struct{}

func DefaultModelPath(modelsDir string) string {
	return filepath.Join(modelsDir, modelcatalog.DefaultInstallTarget().DefaultFilename)
}

func CanonicalModelPath(requestedPath string, defaultPath string) (string, error) {
	selection, err := resolveModelSelection(requestedPath, defaultPath)
	if err != nil {
		return "", err
	}
	return selection.ResolvedPath, nil
}

func CanonicalModelID(requestedPath string, defaultPath string) (string, error) {
	selection, err := resolveModelSelection(requestedPath, defaultPath)
	if err != nil {
		return "", err
	}
	return modelSelectionID(selection), nil
}

// ResolveOrInstallModelPath returns an existing GGUF model path or installs the default model when allowed.
func (ModelCache) ResolveOrInstallModelPath(ctx context.Context, requestedPath string, defaultPath string, allowAutoDownload bool, reporter Reporter) (string, string, error) {
	selection, err := resolveModelSelection(requestedPath, defaultPath)
	if err != nil {
		return "", "", err
	}

	if info, err := os.Stat(selection.ResolvedPath); err == nil {
		if !info.IsDir() {
			return selection.ResolvedPath, "", nil
		}

		if allowAutoDownload && selection.AutoDownload {
			note, err := installEmbeddingModel(ctx, selection.ResolvedPath, selection.Target, reporter)
			if err != nil {
				return "", "", err
			}
			return selection.ResolvedPath, note, nil
		}

		nestedPath, err := findSingleGGUFFile(selection.ResolvedPath)
		if err == nil {
			return nestedPath, fmt.Sprintf("using model file found in %s", selection.ResolvedPath), nil
		}

		return "", "", fmt.Errorf("model path is a directory, expected a GGUF file: %s", selection.ResolvedPath)
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("stat model path: %w", err)
	}

	if allowAutoDownload && selection.AutoDownload {
		note, err := installEmbeddingModel(ctx, selection.ResolvedPath, selection.Target, reporter)
		if err != nil {
			return "", "", err
		}
		return selection.ResolvedPath, note, nil
	}

	return "", "", fmt.Errorf(
		"model file not found: %s; pass --model with an existing GGUF file or use a known model id such as embeddinggemma or qwen3",
		selection.ResolvedPath,
	)
}

type modelSelection struct {
	ResolvedPath string
	Target       modelcatalog.InstallTarget
	AutoDownload bool
}

func resolveModelSelection(requestedPath string, defaultPath string) (modelSelection, error) {
	defaultResolvedPath, err := filepath.Abs(defaultPath)
	if err != nil {
		return modelSelection{}, fmt.Errorf("resolve default model path: %w", err)
	}

	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return modelSelection{
			ResolvedPath: defaultResolvedPath,
			Target:       modelcatalog.DefaultInstallTarget(),
			AutoDownload: true,
		}, nil
	}

	if target, ok := modelcatalog.ResolveInstallTarget(requestedPath); ok {
		return modelSelection{
			ResolvedPath: filepath.Join(filepath.Dir(defaultResolvedPath), target.DefaultFilename),
			Target:       target,
			AutoDownload: true,
		}, nil
	}

	resolvedPath, err := filepath.Abs(requestedPath)
	if err != nil {
		return modelSelection{}, fmt.Errorf("resolve model path: %w", err)
	}

	selection := modelSelection{ResolvedPath: resolvedPath}
	if filepath.Clean(resolvedPath) == filepath.Clean(defaultResolvedPath) {
		selection.Target = modelcatalog.DefaultInstallTarget()
		selection.AutoDownload = true
		return selection, nil
	}
	if target, ok := modelcatalog.RecognizeInstallTargetForPath(resolvedPath); ok {
		selection.Target = target
		selection.AutoDownload = true
	}
	return selection, nil
}

func installEmbeddingModel(ctx context.Context, destPath string, target modelcatalog.InstallTarget, reporter Reporter) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	if info, err := os.Stat(destPath); err == nil {
		if !info.IsDir() {
			if err := validateGGUFFile(destPath); err != nil {
				return "", fmt.Errorf("validate cached model: %w", err)
			}
			return fmt.Sprintf("using cached model from %s", destPath), nil
		}

		note, err := repairInstalledModel(destPath, reporter)
		if err != nil {
			return "", err
		}
		return note, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat model path: %w", err)
	}

	url := strings.TrimSpace(os.Getenv("SEMSEARCH_MODEL_URL"))
	if url == "" {
		url = target.DownloadURL
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(destPath), filepath.Base(destPath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create model temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	if reporter != nil {
		reporter.Logf("downloading %s model from %s to %s", target.DisplayName, url, destPath)
	}

	progress := defaultProgressTracker
	if reporter != nil {
		progress = reporter.ProgressTracker("Downloading model")
	}

	if err := downloadModelWithContext(ctx, url, stagingDir, progress); err != nil {
		return "", fmt.Errorf("download %s model from %s: %w", target.DisplayName, url, err)
	}

	modelFile, err := findSingleGGUFFile(stagingDir)
	if err != nil {
		return "", fmt.Errorf("locate downloaded model in %s: %w", stagingDir, err)
	}

	if err := validateGGUFFile(modelFile); err != nil {
		return "", fmt.Errorf("validate downloaded model: %w", err)
	}

	if err := activateModelFile(modelFile, destPath); err != nil {
		return "", fmt.Errorf("activate downloaded %s model: %w", target.DisplayName, err)
	}

	cleanupModelArtifacts(destPath, reporter)
	return fmt.Sprintf("downloaded %s model to %s", target.DisplayName, destPath), nil
}

func modelSelectionID(selection modelSelection) string {
	if selection.Target.ID != "" {
		return selection.Target.ID
	}
	return "custom:" + filepath.ToSlash(filepath.Clean(selection.ResolvedPath))
}

func repairInstalledModel(destPath string, reporter Reporter) (string, error) {
	modelFile, err := findSingleGGUFFile(destPath)
	if err != nil {
		return "", fmt.Errorf("repair cached model in %s: %w", destPath, err)
	}

	if err := validateGGUFFile(modelFile); err != nil {
		return "", fmt.Errorf("validate cached model in %s: %w", modelFile, err)
	}

	if err := activateModelFile(modelFile, destPath); err != nil {
		return "", fmt.Errorf("repair cached model in %s: %w", destPath, err)
	}

	cleanupModelArtifacts(destPath, reporter)
	return fmt.Sprintf("repaired cached model at %s", destPath), nil
}

func findSingleGGUFFile(root string) (string, error) {
	var matches []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".gguf") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no GGUF files found")
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("found multiple GGUF files: %s", strings.Join(matches, ", "))
	}
}

func validateGGUFFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if string(header) != "GGUF" {
		return fmt.Errorf("unexpected header %q, expected GGUF", string(header))
	}
	return nil
}

func activateModelFile(sourcePath string, destPath string) error {
	sourcePath = filepath.Clean(sourcePath)
	destPath = filepath.Clean(destPath)
	if sourcePath == destPath {
		return nil
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".activate-*")
	if err != nil {
		return fmt.Errorf("create activation temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close activation temp file: %w", err)
	}
	_ = os.Remove(tmpPath)

	if err := os.Rename(sourcePath, tmpPath); err != nil {
		return fmt.Errorf("move model into staging: %w", err)
	}

	if err := os.RemoveAll(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous model destination: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("move staged model into place: %w", err)
	}

	return nil
}

func cleanupModelArtifacts(destPath string, reporter Reporter) {
	parentDir := filepath.Dir(destPath)
	base := filepath.Base(destPath)
	patterns := []string{
		filepath.Join(parentDir, base+".tmp-*"),
		filepath.Join(parentDir, base+".activate-*"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			if reporter != nil {
				reporter.Logf("skip cleanup for %s: %v", pattern, err)
			}
			continue
		}
		for _, match := range matches {
			if filepath.Clean(match) == filepath.Clean(destPath) {
				continue
			}
			if err := os.RemoveAll(match); err != nil && reporter != nil {
				reporter.Logf("cleanup %s: %v", match, err)
			}
		}
	}
}
