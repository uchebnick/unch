package llamaembed

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/uchebnick/unch/internal/indexing"
	"github.com/uchebnick/unch/internal/modelcatalog"
)

const (
	embeddingGemmaRetrievalQueryPrefix = "task: code retrieval | query: "
	embeddingGemmaDocumentPrefix       = "title: %s | text: %s"

	qwen3CodeRetrievalInstruction = "Given a code search query, retrieve relevant code symbols and documentation that answer the query."
)

type ModelProfile struct {
	modelcatalog.Metadata
	DefaultContextSize int
}

type embeddingBehavior interface {
	ProfileRevision() string
	DefaultPooling() llama.PoolingType
	FormatQuery(text string) string
	FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string
}

type registeredEmbeddingModel struct {
	TargetID string
	Defaults runtimeDefaults
	Behavior embeddingBehavior
}

type runtimeDefaults struct {
	DefaultContextSize int
}

type embeddingGemmaModel struct{}

type qwen3EmbeddingModel struct{}

var embeddingModels = []registeredEmbeddingModel{
	{
		TargetID: "embeddinggemma",
		Defaults: runtimeDefaults{DefaultContextSize: 2048},
		Behavior: embeddingGemmaModel{},
	},
	{
		TargetID: "qwen3",
		Defaults: runtimeDefaults{DefaultContextSize: 8192},
		Behavior: qwen3EmbeddingModel{},
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
	return behaviorForPath(modelPath).DefaultPooling()
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

func behaviorForTargetID(targetID string) embeddingBehavior {
	return registeredModelForTargetID(targetID).Behavior
}

func behaviorForPath(modelPath string) embeddingBehavior {
	return behaviorForTargetID(profileForPath(modelPath).ID)
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

func (embeddingGemmaModel) ProfileRevision() string {
	return "embeddinggemma-v4"
}

func (embeddingGemmaModel) DefaultPooling() llama.PoolingType {
	return llama.PoolingTypeMean
}

func (embeddingGemmaModel) FormatQuery(text string) string {
	text = normalizeText(text)
	return embeddingGemmaRetrievalQueryPrefix + text
}

func (embeddingGemmaModel) FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string {
	title, body := indexedSymbolDocumentParts(path, symbol)
	return fmt.Sprintf(embeddingGemmaDocumentPrefix, normalizeDocumentTitle(title), normalizeText(body))
}

func (qwen3EmbeddingModel) ProfileRevision() string {
	return "qwen3-embedding-v1"
}

func (qwen3EmbeddingModel) DefaultPooling() llama.PoolingType {
	return llama.PoolingTypeLast
}

func (qwen3EmbeddingModel) FormatQuery(text string) string {
	return formatQwen3Query(qwen3CodeRetrievalInstruction, text)
}

func (qwen3EmbeddingModel) FormatIndexedSymbolDocument(path string, symbol indexing.IndexedSymbol) string {
	title, body := indexedSymbolDocumentParts(path, symbol)
	return formatQwen3Document(title, body)
}

func indexedSymbolDocumentParts(path string, symbol indexing.IndexedSymbol) (string, string) {
	kind := normalizeText(symbol.Kind)
	name := normalizeText(symbol.Name)
	qualifiedName := normalizeText(symbol.QualifiedName)
	signature := normalizeText(symbol.Signature)
	documentation := normalizeText(symbol.Documentation)
	fileContext := normalizeText(symbol.FileContext)
	bodyText := normalizeText(symbol.Body)

	var body strings.Builder
	body.WriteString("Path: ")
	body.WriteString(path)
	if kind != "" {
		body.WriteString("\nKind: ")
		body.WriteString(kind)
	}
	if name != "" {
		body.WriteString("\nName: ")
		body.WriteString(name)
	}
	if qualifiedName != "" && qualifiedName != name {
		body.WriteString("\nQualified name: ")
		body.WriteString(qualifiedName)
	}
	if signature != "" {
		body.WriteString("\nSignature:\n")
		body.WriteString(signature)
	}
	if documentation != "" {
		body.WriteString("\nDocumentation:\n")
		body.WriteString(documentation)
	}
	if fileContext != "" {
		body.WriteString("\nFile context:\n")
		body.WriteString(fileContext)
	}
	if bodyText != "" {
		body.WriteString("\nBody snippet:\n")
		body.WriteString(bodyText)
	}

	title := normalizeDocumentTitle(strings.TrimSpace(filepath.Base(path)))
	if qualifiedName != "" {
		title = normalizeDocumentTitle(strings.TrimSpace(title + " " + qualifiedName))
	}
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = "symbol"
	}

	return title, body.String()
}

func formatQwen3Query(instruction string, query string) string {
	instruction = normalizeText(instruction)
	query = normalizeText(query)
	return fmt.Sprintf("Instruct: %s\nQuery: %s", instruction, query)
}

func formatQwen3Document(title string, text string) string {
	title = normalizeDocumentTitle(title)
	text = normalizeText(text)

	switch {
	case title == "" && text == "":
		return ""
	case title == "":
		return text
	case text == "":
		return "Title: " + title
	default:
		return "Title: " + title + "\n" + text
	}
}

func normalizeDocumentTitle(title string) string {
	title = normalizeText(title)
	title = strings.ReplaceAll(title, "|", "/")
	if title == "" {
		return "none"
	}
	return title
}
