package indexing

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testScanner struct {
	jobs []FileJob
}

func (s testScanner) CollectJob(path string, rel string, source []byte, commentPrefix string, contextPrefix string) (FileJob, bool, error) {
	for _, job := range s.jobs {
		if job.Path == rel {
			return job, len(job.Symbols) > 0, nil
		}
	}
	return FileJob{Path: rel, SourcePath: path}, false, nil
}

type testRepo struct {
	snapshotID        int64
	currentSnapshotID int64
	existing          map[string]bool
	copyCounts        map[string]int
	added             []string
	inserts           []string
	copies            []string
	models            []string
	activated         int64
	cleaned           bool
}

func (r *testRepo) BeginSnapshot(ctx context.Context, provider string, modelID string) (int64, error) {
	r.models = append(r.models, provider+"/"+modelID)
	return r.snapshotID, nil
}
func (r *testRepo) CurrentSnapshotIfAny(ctx context.Context, provider string, modelID string) (int64, bool, error) {
	if r.currentSnapshotID == 0 {
		return 0, false, nil
	}
	return r.currentSnapshotID, true, nil
}
func (r *testRepo) ActivateSnapshot(ctx context.Context, provider string, modelID string, snapshotID int64) error {
	r.models = append(r.models, provider+"/"+modelID)
	r.activated = snapshotID
	return nil
}
func (r *testRepo) EmbeddingExists(ctx context.Context, provider string, modelID string, commentHash string) (bool, error) {
	return r.existing[commentHash], nil
}
func (r *testRepo) AddEmbedding(ctx context.Context, provider string, modelID string, commentHash string, embedding []float32) error {
	r.added = append(r.added, commentHash)
	return nil
}
func (r *testRepo) InsertSymbol(ctx context.Context, snapshotID int64, provider string, modelID string, path string, symbol IndexedSymbol, commentHash string) error {
	r.inserts = append(r.inserts, path)
	return nil
}
func (r *testRepo) CopyPathFromSnapshot(ctx context.Context, provider string, modelID string, srcSnapshotID, dstSnapshotID int64, path string) (int, error) {
	r.copies = append(r.copies, path)
	if r.copyCounts != nil {
		return r.copyCounts[path], nil
	}
	return 0, nil
}
func (r *testRepo) CleanupInactiveSnapshots(ctx context.Context) error {
	r.cleaned = true
	return nil
}
func (r *testRepo) CleanupUnusedEmbeddings(ctx context.Context) error { return nil }

type testHashStore struct {
	inserted map[string]string
}

func (h *testHashStore) InsertFileHash(ctx context.Context, stateVersion int64, path string, contentHash string) error {
	if h.inserted == nil {
		h.inserted = make(map[string]string)
	}
	h.inserted[path] = contentHash
	return nil
}

type testEmbedder struct {
	hashCalls  int
	embedCalls int
}

func (e *testEmbedder) IndexedSymbolHash(path string, symbol IndexedSymbol) string {
	e.hashCalls++
	return path + ":" + symbol.QualifiedName
}

func (e *testEmbedder) EmbedIndexedSymbol(path string, symbol IndexedSymbol) ([]float32, error) {
	e.embedCalls++
	return []float32{1, 2, 3}, nil
}

type testBatchEmbedder struct {
	mu         sync.Mutex
	calls      int
	batchSizes []int
	errForCall map[int]error
	splitLarge bool
}

func (e *testBatchEmbedder) IndexedSymbolHash(path string, symbol IndexedSymbol) string {
	return path + ":" + symbol.QualifiedName
}

func (e *testBatchEmbedder) EmbedIndexedSymbol(path string, symbol IndexedSymbol) ([]float32, error) {
	return []float32{1, 2, 3}, nil
}

func (e *testBatchEmbedder) EmbedIndexedSymbolBatch(items []EmbedItem) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.batchSizes = append(e.batchSizes, len(items))
	err := e.errForCall[call]
	splitLarge := e.splitLarge && len(items) > 1
	e.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if splitLarge {
		return nil, testEmbeddingError{split: true}
	}

	vectors := make([][]float32, len(items))
	for i := range items {
		vectors[i] = []float32{float32(i), 2, 3}
	}
	return vectors, nil
}

type testEmbeddingError struct {
	temporary  bool
	split      bool
	retryAfter time.Duration
}

func (e testEmbeddingError) Error() string { return "test embedding error" }
func (e testEmbeddingError) Temporary() bool {
	return e.temporary
}
func (e testEmbeddingError) RetryAfter() time.Duration {
	if e.retryAfter > 0 {
		return e.retryAfter
	}
	if e.temporary {
		return time.Nanosecond
	}
	return 0
}
func (e testEmbeddingError) SplitBatch() bool {
	return e.split
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

	root := t.TempDir()
	sourcePath := writeServiceTestFile(t, root, "a.go", "package demo\n")

	scanner := testScanner{
		jobs: []FileJob{{Path: "a.go", SourcePath: sourcePath, Symbols: []IndexedSymbol{
			{Line: 1, Kind: "function", Name: "First", QualifiedName: "First", Documentation: "first", Body: "func A() {}", FileContext: "context"},
			{Line: 2, Kind: "function", Name: "Second", QualifiedName: "Second", Documentation: "second", Body: "func B() {}", FileContext: "context"},
		}}},
	}
	repo := &testRepo{
		snapshotID: 2,
		existing:   map[string]bool{"a.go:First": true},
	}
	reporter := &testReporter{}
	embedder := &testEmbedder{}

	service := Service{
		Scanner:  scanner,
		Repo:     repo,
		Embedder: embedder,
		Hashes:   &testHashStore{},
	}

	result, err := service.Run(context.Background(), Params{
		Root:                 root,
		GitignorePath:        filepath.Join(root, ".gitignore"),
		CommentPrefix:        "@search:",
		ContextPrefix:        "@filectx:",
		ModelID:              "qwen3",
		FileHashStateVersion: 1,
	}, reporter)
	if err != nil {
		t.Fatalf("Service.Run() error: %v", err)
	}

	if result.IndexedFiles != 1 || result.IndexedSymbols != 2 {
		t.Fatalf("Service.Run() result = %+v", result)
	}
	if result.Version != 2 {
		t.Fatalf("Service.Run().Version = %d, want 2", result.Version)
	}
	if len(repo.added) != 1 {
		t.Fatalf("expected one new embedding, got %v", repo.added)
	}
	if embedder.hashCalls != 2 {
		t.Fatalf("expected two hash calls, got %d", embedder.hashCalls)
	}
	if embedder.embedCalls != 1 {
		t.Fatalf("expected one embedding call after cache check, got %d", embedder.embedCalls)
	}
	if len(repo.inserts) != 2 {
		t.Fatalf("expected two inserts, got %v", repo.inserts)
	}
	for _, got := range repo.inserts {
		if got != "a.go" {
			t.Fatalf("expected relative insert path, got %q", got)
		}
	}
	if repo.activated != 2 || !repo.cleaned {
		t.Fatalf("expected version activation and cleanup, got activated=%d cleaned=%v", repo.activated, repo.cleaned)
	}
	if len(repo.models) < 2 || repo.models[0] != "llama.cpp/qwen3" || repo.models[len(repo.models)-1] != "llama.cpp/qwen3" {
		t.Fatalf("expected model tracking for qwen3, got %v", repo.models)
	}
	if reporter.progressCalls == 0 {
		t.Fatalf("expected progress updates")
	}
}

func TestServiceRunHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := writeServiceTestFile(t, root, "a.go", "package demo\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := Service{
		Scanner: testScanner{
			jobs: []FileJob{{Path: "a.go", SourcePath: sourcePath, Symbols: []IndexedSymbol{{Line: 1, Kind: "function", Name: "A", QualifiedName: "A"}}}},
		},
		Repo:     &testRepo{snapshotID: 1, existing: map[string]bool{}},
		Embedder: &testEmbedder{},
		Hashes:   &testHashStore{},
	}

	if _, err := service.Run(ctx, Params{Root: root, GitignorePath: filepath.Join(root, ".gitignore")}, nil); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestServiceRunCopiesUnchangedFilesFromCurrentSnapshot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	aPath := writeServiceTestFile(t, root, "a.go", "package demo\n")
	bPath := writeServiceTestFile(t, root, "b.go", "package demo\n")
	aHash, binary, err := hashSourceFile(aPath)
	if err != nil {
		t.Fatalf("hashSourceFile(a.go) error: %v", err)
	}
	if binary {
		t.Fatalf("a.go unexpectedly detected as binary")
	}

	scanner := testScanner{
		jobs: []FileJob{
			{Path: "a.go", SourcePath: aPath, Symbols: []IndexedSymbol{
				{Line: 1, Kind: "function", Name: "A", QualifiedName: "A"},
			}},
			{Path: "b.go", SourcePath: bPath, Symbols: []IndexedSymbol{
				{Line: 2, Kind: "function", Name: "B", QualifiedName: "B"},
			}},
		},
	}
	repo := &testRepo{
		snapshotID:        8,
		currentSnapshotID: 3,
		existing:          map[string]bool{"b.go:B": true},
		copyCounts:        map[string]int{"a.go": 1},
	}
	hashes := &testHashStore{}
	embedder := &testEmbedder{}

	service := Service{
		Scanner:  scanner,
		Repo:     repo,
		Embedder: embedder,
		Hashes:   hashes,
	}

	result, err := service.Run(context.Background(), Params{
		Root:                 root,
		GitignorePath:        filepath.Join(root, ".gitignore"),
		CommentPrefix:        "@search:",
		ContextPrefix:        "@filectx:",
		ModelID:              "embeddinggemma",
		CurrentFileHashes:    map[string]string{"a.go": aHash, "b.go": "old"},
		FileHashStateVersion: 2,
	}, &testReporter{})
	if err != nil {
		t.Fatalf("Service.Run() error: %v", err)
	}

	if result.Version != 8 {
		t.Fatalf("Service.Run().Version = %d, want 8", result.Version)
	}
	if result.IndexedFiles != 2 || result.IndexedSymbols != 2 {
		t.Fatalf("Service.Run() result = %+v", result)
	}
	if len(repo.copies) != 1 || repo.copies[0] != "a.go" {
		t.Fatalf("expected a.go to be copied, got %v", repo.copies)
	}
	if len(repo.inserts) != 1 || repo.inserts[0] != "b.go" {
		t.Fatalf("expected only b.go to be reinserted, got %v", repo.inserts)
	}
	if embedder.embedCalls != 0 {
		t.Fatalf("expected embedding reuse for b.go, got %d embed calls", embedder.embedCalls)
	}
	if hashes.inserted["a.go"] == "" || hashes.inserted["b.go"] == "" {
		t.Fatalf("expected per-file hashes to be inserted, got %#v", hashes.inserted)
	}
}

func TestFlushEmbeddingBatchesRetriesTemporaryErrors(t *testing.T) {
	t.Parallel()

	repo := &testRepo{existing: map[string]bool{}}
	batcher := &testBatchEmbedder{
		errForCall: map[int]error{
			1: testEmbeddingError{temporary: true},
		},
	}
	state := runState{
		service: Service{
			Repo:     repo,
			Embedder: batcher,
		},
		ctx:      context.Background(),
		provider: "openrouter",
		modelID:  "qwen",
	}

	err := state.flushEmbeddingBatches(batcher, []pendingEmbedding{
		{path: "a.go", symbol: IndexedSymbol{Line: 1, QualifiedName: "A"}, hash: "a"},
		{path: "b.go", symbol: IndexedSymbol{Line: 2, QualifiedName: "B"}, hash: "b"},
	})
	if err != nil {
		t.Fatalf("flushEmbeddingBatches() error: %v", err)
	}
	if batcher.calls != 2 {
		t.Fatalf("batch calls = %d, want 2", batcher.calls)
	}
	if len(repo.added) != 2 {
		t.Fatalf("stored embeddings = %v, want 2", repo.added)
	}
}

func TestFlushEmbeddingBatchesSplitsFailedBatches(t *testing.T) {
	t.Parallel()

	repo := &testRepo{existing: map[string]bool{}}
	batcher := &testBatchEmbedder{splitLarge: true}
	state := runState{
		service: Service{
			Repo:     repo,
			Embedder: batcher,
		},
		ctx:      context.Background(),
		provider: "openrouter",
		modelID:  "qwen",
	}

	err := state.flushEmbeddingBatches(batcher, []pendingEmbedding{
		{path: "a.go", symbol: IndexedSymbol{Line: 1, QualifiedName: "A"}, hash: "a"},
		{path: "b.go", symbol: IndexedSymbol{Line: 2, QualifiedName: "B"}, hash: "b"},
	})
	if err != nil {
		t.Fatalf("flushEmbeddingBatches() error: %v", err)
	}
	if len(repo.added) != 2 {
		t.Fatalf("stored embeddings = %v, want 2", repo.added)
	}
	if len(batcher.batchSizes) < 3 {
		t.Fatalf("batch sizes = %v, want initial batch plus split retries", batcher.batchSizes)
	}
	if batcher.batchSizes[0] != 2 {
		t.Fatalf("first batch size = %d, want 2", batcher.batchSizes[0])
	}
}

func TestRetryEmbeddingBatchUpdatesAttemptAndQueuedAt(t *testing.T) {
	t.Parallel()

	retries, err := retryEmbeddingBatch(context.Background(), embeddingBatchRequest{items: []embeddingRequest{
		{item: pendingEmbedding{path: "a.go", symbol: IndexedSymbol{Line: 1, QualifiedName: "A"}, hash: "a"}, attempt: 1},
		{item: pendingEmbedding{path: "b.go", symbol: IndexedSymbol{Line: 2, QualifiedName: "B"}, hash: "b"}, attempt: 1},
	}}, testEmbeddingError{split: true})
	if err != nil {
		t.Fatalf("retryEmbeddingBatch() error: %v", err)
	}
	if len(retries) != 2 {
		t.Fatalf("retry batch count = %d, want 2", len(retries))
	}
	for _, retry := range retries {
		for _, item := range retry.items {
			if item.attempt != 2 {
				t.Fatalf("retry attempt = %d, want 2", item.attempt)
			}
			if item.queuedAt.IsZero() {
				t.Fatalf("retry queuedAt was not set")
			}
		}
	}
}

func TestEmbeddingRetryDelayCapsRetryAfter(t *testing.T) {
	t.Parallel()

	delay := embeddingRetryDelay(testEmbeddingError{retryAfter: time.Hour}, 2)
	if delay != 10*time.Second {
		t.Fatalf("retry delay = %s, want 10s", delay)
	}
}

func writeServiceTestFile(t *testing.T, root string, rel string, content string) string {
	t.Helper()

	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return path
}
