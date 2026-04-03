package search

import (
	"slices"
	"testing"
)

func TestNormalizeSearchText(t *testing.T) {
	t.Parallel()

	got := normalizeSearchText("  Global_model-cache!!  ")
	want := "global model cache"
	if got != want {
		t.Fatalf("normalizeSearchText() = %q, want %q", got, want)
	}
}

func TestSearchQueryTokensDeduplicates(t *testing.T) {
	t.Parallel()

	got := searchQueryTokens("database Database sqlite sqlite")
	want := []string{"database", "sqlite"}
	if !slices.Equal(got, want) {
		t.Fatalf("searchQueryTokens() = %v, want %v", got, want)
	}
}

func TestShouldPreferLexicalSearch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  bool
	}{
		{query: "", want: false},
		{query: "hui", want: true},
		{query: "RunCLI", want: true},
		{query: "internal/runtime/model_cache.go", want: true},
		{query: "qwen install", want: false},
		{query: "qwen3 install", want: false},
		{query: "global model cache", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			if got := shouldPreferLexicalSearch(tt.query); got != tt.want {
				t.Fatalf("shouldPreferLexicalSearch(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestLooksCodeLikeQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query string
		want  bool
	}{
		{query: "", want: false},
		{query: "RunCLI", want: true},
		{query: "internal/cli.go", want: true},
		{query: "llama2", want: true},
		{query: "global model cache", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			if got := looksCodeLikeQuery(tt.query); got != tt.want {
				t.Fatalf("looksCodeLikeQuery(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestExpandQueryTokenIncludesSynonyms(t *testing.T) {
	t.Parallel()

	var got []string
	for _, item := range expandQueryToken("database") {
		got = append(got, item.token)
	}

	for _, want := range []string{"database", "databases", "db", "sqlite", "sql"} {
		if !slices.Contains(got, want) {
			t.Fatalf("expandQueryToken(database) is missing %q from %v", want, got)
		}
	}
}

func TestLexicalMatchScore(t *testing.T) {
	t.Parallel()

	databaseScore := lexicalMatchScore("database", SearchResult{
		Path:          "internal/repository.go",
		Kind:          "type",
		Name:          "SQLiteStore",
		Documentation: "SQLite repository schema",
	})
	if databaseScore <= 0 {
		t.Fatalf("expected positive score for database synonym match, got %f", databaseScore)
	}

	runCLIScore := lexicalMatchScore("RunCLI", SearchResult{
		Path:          "internal/cli.go",
		Kind:          "function",
		Name:          "RunCLI",
		QualifiedName: "RunCLI",
		Documentation: "RunCLI dispatches search subcommand",
	})
	if runCLIScore <= 0 {
		t.Fatalf("expected positive score for exact lexical match, got %f", runCLIScore)
	}

	noiseScore := lexicalMatchScore("hui", SearchResult{
		Path:          "internal/repository.go",
		Documentation: "SQLite repository schema",
	})
	if noiseScore != 0 {
		t.Fatalf("expected zero score for unrelated text, got %f", noiseScore)
	}
}

func TestNormalizeSearchMode(t *testing.T) {
	t.Parallel()

	got, err := NormalizeMode("SeMaNtIc")
	if err != nil {
		t.Fatalf("NormalizeMode returned error: %v", err)
	}
	if got != "semantic" {
		t.Fatalf("NormalizeMode() = %q, want semantic", got)
	}

	if _, err := NormalizeMode("unknown"); err == nil {
		t.Fatalf("expected error for unknown mode")
	}
}

func TestShouldPreferLexicalResults(t *testing.T) {
	t.Parallel()

	semantic := []Result{{SearchResult: SearchResult{Distance: 0.9}}}
	lexical := []Result{{LexicalScore: 0.7}}
	if !shouldPreferLexicalResults("RunCLI", semantic, lexical) {
		t.Fatalf("expected lexical results to win when semantic top distance is weak")
	}

	semantic = []Result{{SearchResult: SearchResult{Distance: 0.72}}}
	lexical = []Result{{LexicalScore: 0.95}}
	if shouldPreferLexicalResults("qwen install", semantic, lexical) {
		t.Fatalf("did not expect lexical results to win for natural-language query")
	}

	semantic = []Result{{SearchResult: SearchResult{Distance: 0.72}}}
	lexical = []Result{{LexicalScore: 0.95}}
	if shouldPreferLexicalResults("global model cache", semantic, lexical) {
		t.Fatalf("did not expect lexical results to win when semantic top distance is strong")
	}
}

func TestSemanticSortDistanceUsesFileWeight(t *testing.T) {
	t.Parallel()

	unweighted := semanticSortDistance(0.4, 1)
	downweighted := semanticSortDistance(0.4, 0.82)
	if downweighted <= unweighted {
		t.Fatalf("expected downweighted semantic distance to get worse, got %f <= %f", downweighted, unweighted)
	}
}
