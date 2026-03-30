package indexing

import (
	"context"
	"testing"
)

type testScanner struct {
	jobs     []FileJob
	comments map[string][]IndexedComment
	contexts map[string]string
}

func (s testScanner) CollectJobs(root string, gitignorePath string, extraPatterns []string, commentPrefix string, contextPrefix string) ([]FileJob, int, error) {
	total := 0
	for _, job := range s.jobs {
		total += job.CommentsCount
	}
	return s.jobs, total, nil
}

func (s testScanner) ExtractPrefixedBlocks(path string, searchPrefix string, ctxPrefix string) ([]IndexedComment, string, error) {
	return s.comments[path], s.contexts[path], nil
}

type testRepo struct {
	workingVersion int64
	existing       map[string]bool
	added          []string
	upserts        []string
	activated      int64
	cleaned        bool
}

func (r *testRepo) WorkingVersion(ctx context.Context) (int64, error) { return r.workingVersion, nil }
func (r *testRepo) ActivateVersion(ctx context.Context, version int64) error {
	r.activated = version
	return nil
}
func (r *testRepo) EmbeddingExists(ctx context.Context, commentHash string) (bool, error) {
	return r.existing[commentHash], nil
}
func (r *testRepo) AddEmbedding(ctx context.Context, commentHash string, embedding []float32) error {
	r.added = append(r.added, commentHash)
	return nil
}
func (r *testRepo) UpsertComment(ctx context.Context, path string, line int, commentHash string, version int64) error {
	r.upserts = append(r.upserts, path)
	return nil
}
func (r *testRepo) CleanupOldVersions(ctx context.Context, activeVersion int64) error {
	r.cleaned = true
	return nil
}
func (r *testRepo) CleanupUnusedEmbeddings(ctx context.Context) error { return nil }

type testEmbedder struct{}

func (testEmbedder) EmbedIndexedComment(path string, comment string, commentContext string, followingText string) (string, []float32, error) {
	return path + ":" + comment, []float32{1, 2, 3}, nil
}

type testReporter struct {
	progressCalls int
}

func (r *testReporter) Logf(format string, args ...any) {}
func (r *testReporter) CountProgress(label string, current, total int) {
	r.progressCalls++
}

func TestServiceRunIndexesComments(t *testing.T) {
	t.Parallel()

	scanner := testScanner{
		jobs: []FileJob{{Path: "/tmp/a.go", CommentsCount: 2}},
		comments: map[string][]IndexedComment{
			"/tmp/a.go": {
				{Line: 1, Text: "first", FollowingText: "func A() {}"},
				{Line: 2, Text: "second", FollowingText: "func B() {}"},
			},
		},
		contexts: map[string]string{"/tmp/a.go": "context"},
	}
	repo := &testRepo{
		workingVersion: 2,
		existing:       map[string]bool{"/tmp/a.go:first": true},
	}
	reporter := &testReporter{}

	service := Service{
		Scanner:  scanner,
		Repo:     repo,
		Embedder: testEmbedder{},
	}

	result, err := service.Run(context.Background(), Params{
		Root:          "/tmp",
		GitignorePath: "/tmp/.gitignore",
		CommentPrefix: "@search:",
		ContextPrefix: "@filectx:",
	}, reporter)
	if err != nil {
		t.Fatalf("Service.Run() error: %v", err)
	}

	if result.IndexedFiles != 1 || result.IndexedComments != 2 {
		t.Fatalf("Service.Run() result = %+v", result)
	}
	if result.Version != 2 {
		t.Fatalf("Service.Run().Version = %d, want 2", result.Version)
	}
	if len(repo.added) != 1 {
		t.Fatalf("expected one new embedding, got %v", repo.added)
	}
	if len(repo.upserts) != 2 {
		t.Fatalf("expected two upserts, got %v", repo.upserts)
	}
	if repo.activated != 2 || !repo.cleaned {
		t.Fatalf("expected version activation and cleanup, got activated=%d cleaned=%v", repo.activated, repo.cleaned)
	}
	if reporter.progressCalls == 0 {
		t.Fatalf("expected progress updates")
	}
}

func TestServiceRunHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := Service{
		Scanner: testScanner{
			jobs: []FileJob{{Path: "/tmp/a.go", CommentsCount: 1}},
		},
		Repo:     &testRepo{workingVersion: 1, existing: map[string]bool{}},
		Embedder: testEmbedder{},
	}

	if _, err := service.Run(ctx, Params{}, nil); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}
