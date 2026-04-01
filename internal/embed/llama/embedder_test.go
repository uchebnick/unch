package llamaembed

import (
	"strings"
	"testing"

	"github.com/uchebnick/unch/internal/indexing"
)

func TestFormatIndexedSymbolDocumentIncludesMetadataAndBody(t *testing.T) {
	t.Parallel()

	doc := formatIndexedSymbolDocument(
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

	got := formatEmbeddingGemmaQuery("global model cache")
	want := "task: code retrieval | query: global model cache"
	if got != want {
		t.Fatalf("formatEmbeddingGemmaQuery() = %q, want %q", got, want)
	}
}

func TestNormalizeEmbeddingGemmaTitle(t *testing.T) {
	t.Parallel()

	if got := normalizeEmbeddingGemmaTitle("internal/model|cache.go"); got != "internal/model/cache.go" {
		t.Fatalf("normalizeEmbeddingGemmaTitle() = %q", got)
	}
	if got := normalizeEmbeddingGemmaTitle(""); got != "none" {
		t.Fatalf("normalizeEmbeddingGemmaTitle(empty) = %q", got)
	}
}

func TestHashCommentIsStable(t *testing.T) {
	t.Parallel()

	hashA := hashComment("same comment")
	hashB := hashComment("same comment")
	hashC := hashComment("other comment")

	if hashA != hashB {
		t.Fatalf("expected identical comments to have identical hashes")
	}
	if hashA == hashC {
		t.Fatalf("expected different comments to have different hashes")
	}
}
