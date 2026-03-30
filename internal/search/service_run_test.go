package search

import (
	"context"
	"errors"
	"testing"
)

type mockRepo struct {
	semantic []SearchResult
	lexical  []SearchResult
}

func (m mockRepo) SearchCurrent(ctx context.Context, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	return m.semantic, nil
}

func (m mockRepo) ListCurrentComments(ctx context.Context) ([]SearchResult, error) {
	return m.lexical, nil
}

type mockEmbedder struct {
	err error
}

func (m mockEmbedder) EmbedQuery(text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []float32{1, 2, 3}, nil
}

type mockScanner struct {
	texts map[int]string
}

func (m mockScanner) ReadSearchResultContent(path string, line int, commentPrefix string, contextPrefix string) (string, string, error) {
	return m.texts[line], "", nil
}

func TestServiceRunSemanticMode(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo: mockRepo{
			semantic: []SearchResult{
				{Path: "b.go", Line: 20, Distance: 0.91},
				{Path: "a.go", Line: 10, Distance: 0.30},
				{Path: "c.go", Line: 30, Distance: 0.50},
			},
		},
		Embedder: mockEmbedder{},
		Scanner: mockScanner{texts: map[int]string{
			10: "best match",
			20: "too far",
			30: "second match",
		}},
	}

	results, err := service.Run(context.Background(), Params{
		QueryText:   "best match",
		Mode:        "semantic",
		Limit:       2,
		MaxDistance: 0.8,
	}, nil)
	if err != nil {
		t.Fatalf("Service.Run(semantic) error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 semantic results, got %d", len(results))
	}
	if results[0].Path != "a.go" || results[1].Path != "c.go" {
		t.Fatalf("unexpected semantic order: %+v", results)
	}
}

func TestServiceRunAutoFallsBackToLexical(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo: mockRepo{
			semantic: nil,
			lexical: []SearchResult{
				{Path: "cli.go", Line: 1},
				{Path: "search.go", Line: 2},
			},
		},
		Embedder: mockEmbedder{},
		Scanner: mockScanner{texts: map[int]string{
			1: "RunCLI dispatches search",
			2: "semantic search entrypoint",
		}},
	}

	results, err := service.Run(context.Background(), Params{
		QueryText: "RunCLI",
		Mode:      "auto",
		Limit:     10,
	}, nil)
	if err != nil {
		t.Fatalf("Service.Run(auto lexical fallback) error: %v", err)
	}
	if len(results) == 0 || results[0].DisplayMetric != "lexical" {
		t.Fatalf("expected lexical results, got %+v", results)
	}
}

func TestServiceRunPropagatesEmbedErrors(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo:     mockRepo{},
		Embedder: mockEmbedder{err: errors.New("boom")},
		Scanner:  mockScanner{},
	}

	if _, err := service.Run(context.Background(), Params{
		QueryText: "query",
		Mode:      "semantic",
	}, nil); err == nil {
		t.Fatalf("expected embed error")
	}
}
