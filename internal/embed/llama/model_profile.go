package llamaembed

import (
	"github.com/hybridgroup/yzma/pkg/llama"
	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/modelcatalog"
)

type ModelProfile struct {
	modelcatalog.Metadata
	DefaultContextSize int
}

type embeddingBehavior interface {
	DefaultPooling() llama.PoolingType
	Formatter() appembed.Formatter
}

type registeredEmbeddingModel struct {
	TargetID string
	Defaults runtimeDefaults
	Pooling  llama.PoolingType
	Format   appembed.Formatter
}

type runtimeDefaults struct {
	DefaultContextSize int
}

var embeddingModels = []registeredEmbeddingModel{
	{
		TargetID: "embeddinggemma",
		Defaults: runtimeDefaults{DefaultContextSize: 2048},
		Pooling:  llama.PoolingTypeMean,
		Format:   appembed.FormatterForModel("embeddinggemma"),
	},
	{
		TargetID: "qwen3",
		Defaults: runtimeDefaults{DefaultContextSize: 8192},
		Pooling:  llama.PoolingTypeLast,
		Format:   appembed.FormatterForModel("qwen3"),
	},
}

// DefaultModelProfile returns the model profile used when --model is omitted.
func DefaultModelProfile() ModelProfile {
	return profileForTarget(modelcatalog.DefaultInstallTarget())
}

// KnownModelProfiles returns the built-in GGUF embedding model profiles supported by the CLI.
func KnownModelProfiles() []ModelProfile {
	targets := modelcatalog.KnownInstallTargets()
	profiles := make([]ModelProfile, 0, len(targets))
	for _, target := range targets {
		profiles = append(profiles, profileForTarget(target))
	}
	return profiles
}

// ResolveKnownModelProfile resolves a short model alias such as "embeddinggemma" or "qwen3".
func ResolveKnownModelProfile(value string) (ModelProfile, bool) {
	target, ok := modelcatalog.ResolveInstallTarget(value)
	if !ok {
		return ModelProfile{}, false
	}

	return profileForTarget(target), true
}

// RecognizeModelProfileForPath returns a built-in model profile when the path matches a known GGUF filename family.
func RecognizeModelProfileForPath(modelPath string) (ModelProfile, bool) {
	target, ok := modelcatalog.RecognizeInstallTargetForPath(modelPath)
	if !ok {
		return ModelProfile{}, false
	}
	return profileForTarget(target), true
}

// DefaultPoolingForModelPath returns the pooling mode that matches the known GGUF embedding model.
func DefaultPoolingForModelPath(modelPath string) llama.PoolingType {
	return registeredModelForTargetID(profileForPath(modelPath).ID).Pooling
}

// DefaultContextSizeForModelPath returns the model-specific context size used when the CLI does not override it.
func DefaultContextSizeForModelPath(modelPath string) int {
	return profileForPath(modelPath).DefaultContextSize
}

func profileForPath(modelPath string) ModelProfile {
	target, ok := modelcatalog.RecognizeInstallTargetForPath(modelPath)
	if !ok {
		target = modelcatalog.DefaultInstallTarget()
	}
	return profileForTarget(target)
}

func formatterForTargetID(targetID string) appembed.Formatter {
	return registeredModelForTargetID(targetID).Format
}

func formatterForPath(modelPath string) appembed.Formatter {
	return formatterForTargetID(profileForPath(modelPath).ID)
}

func profileForTarget(target modelcatalog.InstallTarget) ModelProfile {
	defaults := runtimeDefaultsForTargetID(target.ID)
	return ModelProfile{
		Metadata:           target.Clone(),
		DefaultContextSize: defaults.DefaultContextSize,
	}
}

func runtimeDefaultsForTargetID(targetID string) runtimeDefaults {
	return registeredModelForTargetID(targetID).Defaults
}

func registeredModelForTargetID(targetID string) registeredEmbeddingModel {
	for _, model := range embeddingModels {
		if model.TargetID == targetID {
			return model
		}
	}

	return embeddingModels[0]
}
