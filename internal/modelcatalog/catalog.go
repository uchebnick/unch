package modelcatalog

import (
	"path/filepath"
	"strings"
)

// Metadata is the shared identity layer for a known model.
type Metadata struct {
	ID          string
	DisplayName string
	Aliases     []string
}

func (m Metadata) Clone() Metadata {
	cloned := m
	cloned.Aliases = append([]string(nil), m.Aliases...)
	return cloned
}

// InstallTarget describes how a known model is identified, downloaded, and placed on disk.
// It intentionally excludes runtime defaults such as context size or pooling behavior.
type InstallTarget struct {
	Metadata
	DefaultFilename string
	DownloadURL     string
}

type knownInstallTarget struct {
	InstallTarget
	matchesPath func(name string, full string) bool
}

var knownInstallTargets = []knownInstallTarget{
	{
		InstallTarget: InstallTarget{
			Metadata: Metadata{
				ID:          "embeddinggemma",
				DisplayName: "embeddinggemma-300m",
				Aliases:     []string{"default", "embeddinggemma", "gemma", "embeddinggemma-300m"},
			},
			DefaultFilename: "embeddinggemma-300m.gguf",
			DownloadURL:     "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf?download=true",
		},
		matchesPath: func(name string, full string) bool {
			return strings.Contains(name, "embeddinggemma") || strings.Contains(full, "embeddinggemma")
		},
	},
	{
		InstallTarget: InstallTarget{
			Metadata: Metadata{
				ID:          "qwen3",
				DisplayName: "Qwen3-Embedding-0.6B",
				Aliases:     []string{"qwen3", "qwen3-embedding", "qwen3embedding", "qwen"},
			},
			DefaultFilename: "Qwen3-Embedding-0.6B-Q8_0.gguf",
			DownloadURL:     "https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf?download=true",
		},
		matchesPath: func(name string, full string) bool {
			return strings.Contains(name, "qwen3-embedding") ||
				strings.Contains(name, "qwen3embedding") ||
				(strings.Contains(full, "qwen3") && strings.Contains(full, "embed"))
		},
	},
}

func DefaultInstallTarget() InstallTarget {
	return cloneInstallTarget(knownInstallTargets[0].InstallTarget)
}

func KnownInstallTargets() []InstallTarget {
	targets := make([]InstallTarget, 0, len(knownInstallTargets))
	for _, target := range knownInstallTargets {
		targets = append(targets, cloneInstallTarget(target.InstallTarget))
	}
	return targets
}

func ResolveInstallTarget(value string) (InstallTarget, bool) {
	token := normalizeModelToken(value)
	if token == "" {
		return InstallTarget{}, false
	}

	for _, target := range knownInstallTargets {
		for _, alias := range target.Aliases {
			if token == normalizeModelToken(alias) {
				return cloneInstallTarget(target.InstallTarget), true
			}
		}
	}

	return InstallTarget{}, false
}

func RecognizeInstallTargetForPath(modelPath string) (InstallTarget, bool) {
	name, full := normalizedModelPath(modelPath)
	for _, target := range knownInstallTargets {
		if target.matchesPath(name, full) {
			return cloneInstallTarget(target.InstallTarget), true
		}
	}

	return InstallTarget{}, false
}

func cloneInstallTarget(target InstallTarget) InstallTarget {
	cloned := target
	cloned.Metadata = target.Clone()
	return cloned
}

func normalizedModelPath(modelPath string) (string, string) {
	full := strings.ToLower(strings.TrimSpace(modelPath))
	name := strings.ToLower(filepath.Base(full))
	return name, full
}

func normalizeModelToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("-", "", "_", "", " ", "")
	return replacer.Replace(value)
}
