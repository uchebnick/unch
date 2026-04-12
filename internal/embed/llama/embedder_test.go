package llamaembed

import (
	"strings"
	"testing"

	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/indexing"
)

func TestFormatIndexedSymbolDocumentIncludesMetadataAndBody(t *testing.T) {
	t.Parallel()

	doc := appembed.FormatterForModel("embeddinggemma").FormatIndexedSymbolDocument(
		"/tmp/internal/model_cache.go",
		indexing.IndexedSymbol{
			Kind:          "function",
			Name:          "ResolveOrInstallModelPath",
			QualifiedName: "ModelCache.ResolveOrInstallModelPath",
			Signature:     "func ResolveOrInstallModelPath() string",
			Documentation: "the default embedding model is downloaded once into the user cache",
			FileContext:   "Global GGUF model cache with auto-download",
			Body:          "func resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
		},
	)

	for _, want := range []string{
		"title: model_cache.go ModelCache.ResolveOrInstallModelPath",
		"Kind: function",
		"Name: ResolveOrInstallModelPath",
		"Qualified name: ModelCache.ResolveOrInstallModelPath",
		"Signature:\nfunc ResolveOrInstallModelPath() string",
		"Documentation:\nthe default embedding model is downloaded once into the user cache",
		"File context:\nGlobal GGUF model cache with auto-download",
		"Body snippet:\nfunc resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("formatted document is missing %q in %q", want, doc)
		}
	}
}

func TestFormatEmbeddingGemmaQuery(t *testing.T) {
	t.Parallel()

	got := appembed.FormatterForModel("embeddinggemma").FormatQuery("global model cache")
	want := "task: code retrieval | query: global model cache"
	if got != want {
		t.Fatalf("formatEmbeddingGemmaQuery() = %q, want %q", got, want)
	}
}

func TestFormatQwen3Query(t *testing.T) {
	t.Parallel()

	got := appembed.FormatterForModel("qwen3").FormatQuery("global model cache")
	want := "Instruct: Given a code search query, retrieve relevant code symbols and documentation that answer the query.\nQuery: global model cache"
	if got != want {
		t.Fatalf("formatQwen3Query() = %q, want %q", got, want)
	}
}

func TestFormatQwen3IndexedSymbolDocumentIncludesTitleAndMetadata(t *testing.T) {
	t.Parallel()

	doc := appembed.FormatterForModel("qwen3").FormatIndexedSymbolDocument(
		"/tmp/internal/model_cache.go",
		indexing.IndexedSymbol{
			Kind:          "function",
			Name:          "ResolveOrInstallModelPath",
			QualifiedName: "ModelCache.ResolveOrInstallModelPath",
			Signature:     "func ResolveOrInstallModelPath() string",
			Documentation: "the default embedding model is downloaded once into the user cache",
			FileContext:   "Global GGUF model cache with auto-download",
			Body:          "func resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
		},
	)

	for _, want := range []string{
		"Title: model_cache.go ModelCache.ResolveOrInstallModelPath",
		"Kind: function",
		"Name: ResolveOrInstallModelPath",
		"Qualified name: ModelCache.ResolveOrInstallModelPath",
		"Signature:\nfunc ResolveOrInstallModelPath() string",
		"Documentation:\nthe default embedding model is downloaded once into the user cache",
		"File context:\nGlobal GGUF model cache with auto-download",
		"Body snippet:\nfunc resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("formatted Qwen3 document is missing %q in %q", want, doc)
		}
	}
}

func TestModelForPath(t *testing.T) {
	t.Parallel()

	gemmaID := appembed.FormatterForModel("embeddinggemma").ProfileRevision()
	qwenID := appembed.FormatterForModel("qwen3").ProfileRevision()

	if got := formatterForPath("/tmp/embeddinggemma-300m.gguf").ProfileRevision(); got != gemmaID {
		t.Fatalf("behaviorForPath(gemma) = %q", got)
	}
	if got := formatterForPath("/tmp/Qwen3-Embedding-0.6B-Q8_0.gguf").ProfileRevision(); got != qwenID {
		t.Fatalf("behaviorForPath(qwen3) = %q", got)
	}
}

func TestKnownModelProfiles(t *testing.T) {
	t.Parallel()

	profiles := KnownModelProfiles()
	if len(profiles) < 2 {
		t.Fatalf("KnownModelProfiles() = %d, want at least 2", len(profiles))
	}

	defaultProfile := DefaultModelProfile()
	if defaultProfile.ID != "embeddinggemma" {
		t.Fatalf("DefaultModelProfile().ID = %q", defaultProfile.ID)
	}

	qwenProfile, ok := ResolveKnownModelProfile("qwen3")
	if !ok || qwenProfile.ID != "qwen3" {
		t.Fatalf("ResolveKnownModelProfile(qwen3) = (%#v, %v)", qwenProfile, ok)
	}
	if qwenProfile.DefaultContextSize != 8192 {
		t.Fatalf("ResolveKnownModelProfile(qwen3) default context = %d", qwenProfile.DefaultContextSize)
	}

	gemmaByPath, ok := RecognizeModelProfileForPath("/tmp/embeddinggemma-300m.gguf")
	if !ok || gemmaByPath.ID != "embeddinggemma" {
		t.Fatalf("RecognizeModelProfileForPath(gemma) = (%#v, %v)", gemmaByPath, ok)
	}
	if gemmaByPath.DefaultContextSize != 2048 {
		t.Fatalf("RecognizeModelProfileForPath(gemma) default context = %d", gemmaByPath.DefaultContextSize)
	}
	if got := DefaultContextSizeForModelPath("/tmp/Qwen3-Embedding-0.6B-Q8_0.gguf"); got != 8192 {
		t.Fatalf("DefaultContextSizeForModelPath(qwen3) = %d", got)
	}
}

func TestIndexedSymbolHashIsStable(t *testing.T) {
	t.Parallel()

	formatter := appembed.FormatterForModel("embeddinggemma")
	symbolA := indexing.IndexedSymbol{Name: "A", QualifiedName: "A", Documentation: "same comment"}
	symbolB := indexing.IndexedSymbol{Name: "A", QualifiedName: "A", Documentation: "same comment"}
	symbolC := indexing.IndexedSymbol{Name: "A", QualifiedName: "A", Documentation: "other comment"}

	hashA := appembed.IndexedSymbolHash(formatter, "a.go", symbolA)
	hashB := appembed.IndexedSymbolHash(formatter, "a.go", symbolB)
	hashC := appembed.IndexedSymbolHash(formatter, "a.go", symbolC)

	if hashA != hashB {
		t.Fatalf("expected identical comments to have identical hashes")
	}
	if hashA == hashC {
		t.Fatalf("expected different comments to have different hashes")
	}
}

func TestEffectiveTokenLimitPrefersSmallestPositiveBound(t *testing.T) {
	t.Parallel()

	got := effectiveTokenLimit(2048, 2048, 1024, 1536)
	if got != 1024 {
		t.Fatalf("effectiveTokenLimit() = %d, want 1024", got)
	}
}

func TestEffectiveTokenLimitIgnoresZeroRuntimeValues(t *testing.T) {
	t.Parallel()

	got := effectiveTokenLimit(2048, 0, -1, 2048)
	if got != 2048 {
		t.Fatalf("effectiveTokenLimit() = %d, want 2048", got)
	}
}

func TestEffectiveTokenLimitFallsBackToRuntimeBound(t *testing.T) {
	t.Parallel()

	got := effectiveTokenLimit(0, 4096, 1024, 0)
	if got != 1024 {
		t.Fatalf("effectiveTokenLimit() = %d, want 1024", got)
	}
}
