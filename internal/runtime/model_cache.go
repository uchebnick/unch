package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	getter "github.com/hashicorp/go-getter"
	"github.com/hybridgroup/yzma/pkg/download"
)

type Reporter interface {
	Logf(format string, args ...any)
	ProgressTracker(label string) getter.ProgressTracker
}

const defaultEmbeddingModelURL = "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf?download=true"

type ModelCache struct{}

// ResolveOrInstallModelPath returns an existing GGUF model path or installs the default model when allowed.
func (ModelCache) ResolveOrInstallModelPath(ctx context.Context, requestedPath string, defaultPath string, allowAutoDownload bool, reporter Reporter) (string, string, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		requestedPath = defaultPath
	}

	resolvedPath, err := filepath.Abs(requestedPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve model path: %w", err)
	}

	defaultResolvedPath, err := filepath.Abs(defaultPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve default model path: %w", err)
	}

	if info, err := os.Stat(resolvedPath); err == nil {
		if !info.IsDir() {
			return resolvedPath, "", nil
		}

		if allowAutoDownload && filepath.Clean(resolvedPath) == filepath.Clean(defaultResolvedPath) {
			note, err := installDefaultEmbeddingModel(ctx, resolvedPath, reporter)
			if err != nil {
				return "", "", err
			}
			return resolvedPath, note, nil
		}

		nestedPath, err := findSingleGGUFFile(resolvedPath)
		if err == nil {
			return nestedPath, fmt.Sprintf("using model file found in %s", resolvedPath), nil
		}

		return "", "", fmt.Errorf("model path is a directory, expected a GGUF file: %s", resolvedPath)
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("stat model path: %w", err)
	}

	if allowAutoDownload && filepath.Clean(resolvedPath) == filepath.Clean(defaultResolvedPath) {
		note, err := installDefaultEmbeddingModel(ctx, resolvedPath, reporter)
		if err != nil {
			return "", "", err
		}
		return resolvedPath, note, nil
	}

	return "", "", fmt.Errorf(
		"model file not found: %s; pass --model with an existing GGUF file or omit --model to auto-download the default embedding model",
		resolvedPath,
	)
}

func installDefaultEmbeddingModel(ctx context.Context, destPath string, reporter Reporter) (string, error) {
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
		url = defaultEmbeddingModelURL
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(destPath), filepath.Base(destPath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create model temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	if reporter != nil {
		reporter.Logf("downloading default model from %s to %s", url, destPath)
	}

	progress := download.ProgressTracker
	if reporter != nil {
		progress = reporter.ProgressTracker("Downloading model")
	}

	if err := download.GetModelWithContext(ctx, url, stagingDir, progress); err != nil {
		return "", fmt.Errorf("download default embedding model from %s: %w", url, err)
	}

	modelFile, err := findSingleGGUFFile(stagingDir)
	if err != nil {
		return "", fmt.Errorf("locate downloaded model in %s: %w", stagingDir, err)
	}

	if err := validateGGUFFile(modelFile); err != nil {
		return "", fmt.Errorf("validate downloaded model: %w", err)
	}

	if err := activateModelFile(modelFile, destPath); err != nil {
		return "", fmt.Errorf("activate downloaded model: %w", err)
	}

	cleanupModelArtifacts(destPath, reporter)
	return fmt.Sprintf("downloaded default model to %s", destPath), nil
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
	defer f.Close()

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
