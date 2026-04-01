package search

import (
	"context"
	"errors"
	"testing"
)

type mockPathWeighter map[string]float64

func (m mockPathWeighter) Weight(path string) float64 {
	if weight, ok := m[path]; ok {
		return weight
	}
	return 1
}

type mockRepo struct {
	semantic []SearchResult
	lexical  []SearchResult
}

func (m mockRepo) SearchCurrent(ctx context.Context, modelID string, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	return m.semantic, nil
}

func (m mockRepo) ListCurrentSymbols(ctx context.Context, modelID string) ([]SearchResult, error) {
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

func TestServiceRunSemanticMode(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo: mockRepo{
			semantic: []SearchResult{
				{Path: "b.go", Line: 20, Name: "TooFar", Documentation: "too far", Distance: 0.91},
				{Path: "a.go", Line: 10, Name: "Best", Documentation: "best match", Distance: 0.30},
				{Path: "c.go", Line: 30, Name: "Second", Documentation: "second match", Distance: 0.50},
			},
		},
		Embedder: mockEmbedder{},
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
				{Path: "cli.go", Line: 1, Name: "RunCLI", Documentation: "RunCLI dispatches search"},
				{Path: "search.go", Line: 2, Documentation: "semantic search entrypoint"},
			},
		},
		Embedder: mockEmbedder{},
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
	}

	if _, err := service.Run(context.Background(), Params{
		QueryText: "query",
		Mode:      "semantic",
	}, nil); err == nil {
		t.Fatalf("expected embed error")
	}
}

func TestServiceRunLexicalAppliesFileWeights(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo: mockRepo{
			lexical: []SearchResult{
				{Path: "examples/router_test.go", Line: 10, Name: "NewRouter", QualifiedName: "NewRouter", Documentation: "NewRouter example"},
				{Path: "mux.go", Line: 32, Name: "NewRouter", QualifiedName: "NewRouter", Documentation: "NewRouter returns a new router instance"},
			},
		},
		Embedder:     mockEmbedder{},
		PathWeighter: mockPathWeighter{"examples/router_test.go": 0.82},
	}

	results, err := service.Run(context.Background(), Params{
		QueryText: "NewRouter",
		Mode:      "lexical",
		Limit:     2,
	}, nil)
	if err != nil {
		t.Fatalf("Service.Run(lexical with file weights) error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 lexical results, got %d", len(results))
	}
	if results[0].Path != "mux.go" {
		t.Fatalf("expected canonical file to outrank downweighted example, got %+v", results)
	}
}

func TestServiceRunSemanticAppliesFileWeights(t *testing.T) {
	t.Parallel()

	service := Service{
		Repo: mockRepo{
			semantic: []SearchResult{
				{Path: "examples/router_test.go", Line: 10, Name: "NewRouter", Documentation: "example router", Distance: 0.38},
				{Path: "mux.go", Line: 32, Name: "NewRouter", Documentation: "new router instance", Distance: 0.4},
			},
		},
		Embedder:     mockEmbedder{},
		PathWeighter: mockPathWeighter{"examples/router_test.go": 0.82},
	}

	results, err := service.Run(context.Background(), Params{
		QueryText: "create a new router",
		Mode:      "semantic",
		Limit:     2,
	}, nil)
	if err != nil {
		t.Fatalf("Service.Run(semantic with file weights) error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 semantic results, got %d", len(results))
	}
	if results[0].Path != "mux.go" {
		t.Fatalf("expected canonical file to outrank downweighted example, got %+v", results)
	}
}
