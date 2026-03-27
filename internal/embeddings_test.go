package internal

import (
	"strings"
	"testing"
)

func TestFormatIndexedCommentDocumentIncludesContextAndFollowingCode(t *testing.T) {
	t.Parallel()

	doc := formatIndexedCommentDocument(
		"/tmp/internal/model_cache.go",
		"the default embedding model is downloaded once into the user cache",
		"Global GGUF model cache with auto-download",
		"func resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
	)

	for _, want := range []string{
		"title: model_cache.go",
		"Comment: the default embedding model is downloaded once into the user cache",
		"Context: Global GGUF model cache with auto-download",
		"Following code:\nfunc resolveOrInstallModelPath() {}\nfunc installDefaultEmbeddingModel() {}",
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

	hashA := HashComment("same comment")
	hashB := HashComment("same comment")
	hashC := HashComment("other comment")

	if hashA != hashB {
		t.Fatalf("expected identical comments to have identical hashes")
	}
	if hashA == hashC {
		t.Fatalf("expected different comments to have different hashes")
	}
}
