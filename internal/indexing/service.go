package indexing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Reporter interface {
	Logf(format string, args ...any)
	CountProgress(label string, current, total int)
}

type Scanner interface {
	CollectJob(path string, rel string, source []byte, commentPrefix string, contextPrefix string) (FileJob, bool, error)
}

type Repository interface {
	BeginSnapshot(ctx context.Context, provider string, modelID string) (int64, error)
	CurrentSnapshotIfAny(ctx context.Context, provider string, modelID string) (int64, bool, error)
	ActivateSnapshot(ctx context.Context, provider string, modelID string, snapshotID int64) error
	EmbeddingExists(ctx context.Context, provider string, modelID string, embeddingHash string) (bool, error)
	AddEmbedding(ctx context.Context, provider string, modelID string, embeddingHash string, embedding []float32) error
	InsertSymbol(ctx context.Context, snapshotID int64, provider string, modelID string, path string, symbol IndexedSymbol, embeddingHash string) error
	CopyPathFromSnapshot(ctx context.Context, provider string, modelID string, srcSnapshotID, dstSnapshotID int64, path string) (int, error)
	CleanupInactiveSnapshots(ctx context.Context) error
	CleanupUnusedEmbeddings(ctx context.Context) error
}

type FileHashStore interface {
	InsertFileHash(ctx context.Context, stateVersion int64, path string, contentHash string) error
}

type Embedder interface {
	IndexedSymbolHash(path string, symbol IndexedSymbol) string
	EmbedIndexedSymbol(path string, symbol IndexedSymbol) ([]float32, error)
}

type EmbedItem struct {
	Path   string
	Symbol IndexedSymbol
}

type BatchEmbedder interface {
	EmbedIndexedSymbolBatch(items []EmbedItem) ([][]float32, error)
}

const embeddingBatchSize = 64
const embeddingWorkerCount = 3
const embeddingMaxAttempts = 5
const embeddingRetryBaseDelay = 750 * time.Millisecond
const embeddingBatchMaxDelay = 100 * time.Millisecond

type Params struct {
	Root                 string
	GitignorePath        string
	Excludes             []string
	ContextPrefix        string
	CommentPrefix        string
	Provider             string
	ModelID              string
	CurrentFileHashes    map[string]string
	FileHashStateVersion int64
}

type Result struct {
	Version        int64
	IndexedFiles   int
	IndexedSymbols int
}

type Service struct {
	Scanner  Scanner
	Repo     Repository
	Embedder Embedder
	Hashes   FileHashStore
}

type runState struct {
	service            Service
	ctx                context.Context
	params             Params
	reporter           Reporter
	provider           string
	modelID            string
	currentSnapshotID  int64
	hasCurrentSnapshot bool
	snapshotID         int64
	totalFiles         int
	processedFiles     int
	reusedFiles        int
	reusedSymbols      int
	result             Result
	embeddings         embeddingBuffer
}

type pendingEmbedding struct {
	path   string
	symbol IndexedSymbol
	hash   string
}

type embeddingRequest struct {
	item     pendingEmbedding
	attempt  int
	queuedAt time.Time
}

type embeddingBatchRequest struct {
	items []embeddingRequest
}

type embeddingBatchResult struct {
	request embeddingBatchRequest
	vectors [][]float32
	err     error
}

type embeddingBuffer struct {
	hashes   map[string]struct{}
	requests chan embeddingRequest
	done     chan error
	batcher  BatchEmbedder
}

// Run scans the repository, embeds extracted symbols, and activates the new index version.
func (s Service) Run(ctx context.Context, params Params, reporter Reporter) (Result, error) {
	provider := params.Provider
	if provider == "" {
		provider = "llama.cpp"
	}
	modelID := params.ModelID
	if modelID == "" {
		modelID = "embeddinggemma"
	}

	currentSnapshotID, hasCurrentSnapshot, err := s.Repo.CurrentSnapshotIfAny(ctx, provider, modelID)
	if err != nil {
		return Result{}, fmt.Errorf("read current snapshot: %w", err)
	}

	snapshotID, err := s.Repo.BeginSnapshot(ctx, provider, modelID)
	if err != nil {
		return Result{}, fmt.Errorf("begin snapshot: %w", err)
	}
	if reporter != nil {
		reporter.Logf("provider=%s", provider)
		reporter.Logf("model=%s", modelID)
		reporter.Logf("snapshot id=%d", snapshotID)
	}

	state := runState{
		service:            s,
		ctx:                ctx,
		params:             params,
		reporter:           reporter,
		provider:           provider,
		modelID:            modelID,
		currentSnapshotID:  currentSnapshotID,
		hasCurrentSnapshot: hasCurrentSnapshot,
		snapshotID:         snapshotID,
		totalFiles:         len(params.CurrentFileHashes),
		result:             Result{Version: snapshotID},
		embeddings:         embeddingBuffer{hashes: make(map[string]struct{})},
	}
	state.startEmbeddingPipeline()

	if err := state.walk(); err != nil {
		if pipelineErr := state.finishEmbeddingPipeline(); pipelineErr != nil {
			return Result{}, fmt.Errorf("%w; embedding pipeline: %v", err, pipelineErr)
		}
		return Result{}, err
	}
	if err := state.finalize(); err != nil {
		return Result{}, err
	}

	return state.result, nil
}

func (r *runState) walk() error {
	return walkIndexedPaths(r.params.Root, r.params.GitignorePath, r.params.Excludes, r.handleFile)
}

func (r *runState) handleFile(path string, rel string) error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	default:
	}

	contentHash, binary, err := hashSourceFile(path)
	if err != nil {
		return fmt.Errorf("hash source for %s: %w", rel, err)
	}
	if binary {
		return nil
	}

	if err := r.storeFileHash(rel, contentHash); err != nil {
		return err
	}
	if r.canReuseFile(rel, contentHash) {
		return r.reuseFile(rel)
	}
	return r.reindexFile(path, rel)
}

func (r *runState) storeFileHash(rel string, contentHash string) error {
	if r.service.Hashes == nil || r.params.FileHashStateVersion <= 0 {
		return nil
	}
	if err := r.service.Hashes.InsertFileHash(r.ctx, r.params.FileHashStateVersion, rel, contentHash); err != nil {
		return fmt.Errorf("insert file hash for %s: %w", rel, err)
	}
	return nil
}

func (r *runState) canReuseFile(rel string, contentHash string) bool {
	return r.hasCurrentSnapshot && r.params.CurrentFileHashes[rel] == contentHash
}

func (r *runState) reuseFile(rel string) error {
	copiedSymbols, err := r.service.Repo.CopyPathFromSnapshot(r.ctx, r.provider, r.modelID, r.currentSnapshotID, r.snapshotID, rel)
	if err != nil {
		return fmt.Errorf("copy unchanged file %s: %w", rel, err)
	}
	if copiedSymbols > 0 {
		r.reusedFiles++
		r.reusedSymbols += copiedSymbols
		r.result.IndexedFiles++
		r.result.IndexedSymbols += copiedSymbols
	}
	r.advanceProgress()
	return nil
}

func (r *runState) reindexFile(path string, rel string) error {
	source, binary, err := readSourceFile(path)
	if err != nil {
		return fmt.Errorf("read source for %s: %w", rel, err)
	}
	if binary {
		return nil
	}

	job, ok, err := r.service.Scanner.CollectJob(path, rel, source, r.params.CommentPrefix, r.params.ContextPrefix)
	if err != nil {
		return err
	}
	if !ok {
		r.advanceProgress()
		return nil
	}

	if err := r.indexJob(job); err != nil {
		return err
	}
	r.result.IndexedFiles++
	r.result.IndexedSymbols += len(job.Symbols)
	r.advanceProgress()
	return nil
}

func (r *runState) indexJob(job FileJob) error {
	for _, symbol := range job.Symbols {
		hash := r.service.Embedder.IndexedSymbolHash(job.Path, symbol)
		exists, err := r.service.Repo.EmbeddingExists(r.ctx, r.provider, r.modelID, hash)
		if err != nil {
			return fmt.Errorf("check embedding exists: %w", err)
		}
		if !exists {
			if err := r.queueMissingEmbedding(job.Path, symbol, hash); err != nil {
				return err
			}
		}
		if err := r.service.Repo.InsertSymbol(r.ctx, r.snapshotID, r.provider, r.modelID, job.Path, symbol, hash); err != nil {
			return fmt.Errorf("insert symbol: %w", err)
		}
	}
	return nil
}

func (r *runState) startEmbeddingPipeline() {
	batcher, ok := r.service.Embedder.(BatchEmbedder)
	if !ok {
		return
	}
	r.embeddings.batcher = batcher
	r.embeddings.requests = make(chan embeddingRequest, embeddingBatchSize*embeddingWorkerCount)
	r.embeddings.done = make(chan error, 1)
	go func() {
		r.embeddings.done <- r.runEmbeddingPipeline(batcher, r.embeddings.requests)
	}()
}

func (r *runState) finishEmbeddingPipeline() error {
	if r.embeddings.requests == nil {
		return nil
	}
	close(r.embeddings.requests)
	r.embeddings.requests = nil
	return <-r.embeddings.done
}

func (r *runState) queueMissingEmbedding(path string, symbol IndexedSymbol, hash string) error {
	if _, ok := r.embeddings.hashes[hash]; ok {
		return nil
	}
	r.embeddings.hashes[hash] = struct{}{}
	item := pendingEmbedding{
		path:   path,
		symbol: symbol,
		hash:   hash,
	}

	if r.embeddings.requests != nil {
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case r.embeddings.requests <- embeddingRequest{item: item, attempt: 1, queuedAt: time.Now()}:
			return nil
		}
	}

	vec, err := r.service.Embedder.EmbedIndexedSymbol(path, symbol)
	if err != nil {
		return fmt.Errorf("embed symbol at %s:%d: %w", path, symbol.Line, err)
	}
	if err := r.service.Repo.AddEmbedding(r.ctx, r.provider, r.modelID, hash, vec); err != nil {
		return fmt.Errorf("store embedding: %w", err)
	}
	return nil
}

func (r *runState) flushEmbeddingBatches(batcher BatchEmbedder, batch []pendingEmbedding) error {
	requests := make(chan embeddingRequest, len(batch))
	now := time.Now()
	for _, item := range batch {
		requests <- embeddingRequest{item: item, attempt: 1, queuedAt: now}
	}
	close(requests)
	return r.runEmbeddingPipeline(batcher, requests)
}

func (r *runState) runEmbeddingPipeline(batcher BatchEmbedder, requests <-chan embeddingRequest) error {
	jobs := make(chan embeddingBatchRequest, embeddingWorkerCount)
	results := make(chan embeddingBatchResult, embeddingWorkerCount)

	var workers sync.WaitGroup
	for i := 0; i < embeddingWorkerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			embeddingBatchWorker(r.ctx, batcher, jobs, results)
		}()
	}

	pending := make([]embeddingRequest, 0, embeddingBatchSize)
	queue := make([]embeddingBatchRequest, 0)
	active := 0
	inputClosed := false

	closeWorkers := func() {
		close(jobs)
		workers.Wait()
	}
	finishWithError := func(err error) error {
		closeWorkers()
		drainEmbeddingRequests(r.ctx, requests)
		return err
	}

	for {
		if len(pending) >= embeddingBatchSize {
			queue = append(queue, popEmbeddingBatch(&pending, embeddingBatchSize))
		}
		for len(queue) > 0 && active < embeddingWorkerCount {
			select {
			case <-r.ctx.Done():
				closeWorkers()
				return r.ctx.Err()
			case jobs <- queue[0]:
				queue = queue[1:]
				active++
			}
		}
		if inputClosed && len(pending) == 0 && len(queue) == 0 && active == 0 {
			closeWorkers()
			return nil
		}

		timer := embeddingBatchTimer(pending)
		select {
		case <-r.ctx.Done():
			closeWorkers()
			return r.ctx.Err()
		case req, ok := <-requests:
			if !ok {
				inputClosed = true
				if len(pending) > 0 {
					queue = append(queue, popEmbeddingBatch(&pending, len(pending)))
				}
				continue
			}
			if req.queuedAt.IsZero() {
				req.queuedAt = time.Now()
			}
			pending = append(pending, req)
		case <-timer:
			if len(pending) > 0 {
				queue = append(queue, popEmbeddingBatch(&pending, len(pending)))
			}
		case result := <-results:
			active--
			if result.err != nil {
				retryBatches, err := retryEmbeddingBatch(r.ctx, result.request, result.err)
				if err != nil {
					return finishWithError(err)
				}
				queue = append(queue, retryBatches...)
				continue
			}
			if err := r.storeEmbeddingBatchResult(result); err != nil {
				return finishWithError(err)
			}
		}
	}
}

func drainEmbeddingRequests(ctx context.Context, requests <-chan embeddingRequest) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-requests:
			if !ok {
				return
			}
		}
	}
}

func popEmbeddingBatch(pending *[]embeddingRequest, size int) embeddingBatchRequest {
	if size > len(*pending) {
		size = len(*pending)
	}
	items := make([]embeddingRequest, size)
	copy(items, (*pending)[:size])
	*pending = (*pending)[size:]
	return embeddingBatchRequest{items: items}
}

func embeddingBatchTimer(pending []embeddingRequest) <-chan time.Time {
	if len(pending) == 0 {
		return nil
	}
	delay := embeddingBatchMaxDelay - time.Since(pending[0].queuedAt)
	if delay <= 0 {
		delay = 1
	}
	return time.After(delay)
}

func (r *runState) storeEmbeddingBatchResult(result embeddingBatchResult) error {
	if len(result.vectors) != len(result.request.items) {
		return fmt.Errorf("embed symbols: got %d vectors for %d symbols", len(result.vectors), len(result.request.items))
	}
	for i, vec := range result.vectors {
		item := result.request.items[i].item
		if err := r.service.Repo.AddEmbedding(r.ctx, r.provider, r.modelID, item.hash, vec); err != nil {
			return fmt.Errorf("store embedding: %w", err)
		}
	}
	return nil
}

func embeddingBatchWorker(ctx context.Context, batcher BatchEmbedder, jobs <-chan embeddingBatchRequest, results chan<- embeddingBatchResult) {
	for request := range jobs {
		items := make([]EmbedItem, 0, len(request.items))
		for _, item := range request.items {
			items = append(items, EmbedItem{Path: item.item.path, Symbol: item.item.symbol})
		}
		vectors, err := batcher.EmbedIndexedSymbolBatch(items)
		select {
		case <-ctx.Done():
			return
		case results <- embeddingBatchResult{request: request, vectors: vectors, err: err}:
		}
	}
}

func retryEmbeddingBatch(ctx context.Context, request embeddingBatchRequest, err error) ([]embeddingBatchRequest, error) {
	if len(request.items) == 0 {
		return nil, nil
	}

	nextAttempt := maxEmbeddingAttempt(request.items) + 1
	if nextAttempt > embeddingMaxAttempts {
		return nil, fmt.Errorf("embed symbols after %d attempts: %w", embeddingMaxAttempts, err)
	}

	if shouldSplitEmbeddingBatch(err) && len(request.items) > 1 {
		return splitEmbeddingBatchForRetry(request, nextAttempt), nil
	}

	if !isRetryableEmbeddingError(err) && len(request.items) <= 1 {
		return nil, fmt.Errorf("embed symbol at %s:%d: %w", request.items[0].item.path, request.items[0].item.symbol.Line, err)
	}

	if delay := embeddingRetryDelay(err, nextAttempt); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	retry := make([]embeddingRequest, len(request.items))
	now := time.Now()
	for i, item := range request.items {
		item.attempt = nextAttempt
		item.queuedAt = now
		retry[i] = item
	}
	return []embeddingBatchRequest{{items: retry}}, nil
}

func splitEmbeddingBatchForRetry(request embeddingBatchRequest, nextAttempt int) []embeddingBatchRequest {
	if len(request.items) <= 1 {
		item := request.items[0]
		item.attempt = nextAttempt
		item.queuedAt = time.Now()
		return []embeddingBatchRequest{{items: []embeddingRequest{item}}}
	}

	items := make([]embeddingRequest, len(request.items))
	now := time.Now()
	for i, item := range request.items {
		item.attempt = nextAttempt
		item.queuedAt = now
		items[i] = item
	}
	mid := len(items) / 2
	return []embeddingBatchRequest{
		{items: items[:mid]},
		{items: items[mid:]},
	}
}

func maxEmbeddingAttempt(items []embeddingRequest) int {
	maxAttempt := 0
	for _, item := range items {
		if item.attempt > maxAttempt {
			maxAttempt = item.attempt
		}
	}
	return maxAttempt
}

type retryAfterEmbeddingError interface {
	RetryAfter() time.Duration
}

type temporaryEmbeddingError interface {
	Temporary() bool
}

type splitBatchEmbeddingError interface {
	SplitBatch() bool
}

func isRetryableEmbeddingError(err error) bool {
	if err == nil {
		return false
	}
	var retryAfter retryAfterEmbeddingError
	if errors.As(err, &retryAfter) && retryAfter.RetryAfter() > 0 {
		return true
	}
	var temporary temporaryEmbeddingError
	if errors.As(err, &temporary) {
		return temporary.Temporary()
	}
	return false
}

func shouldSplitEmbeddingBatch(err error) bool {
	if err == nil {
		return false
	}
	var splitter splitBatchEmbeddingError
	if errors.As(err, &splitter) {
		return splitter.SplitBatch()
	}
	return !isRetryableEmbeddingError(err)
}

func embeddingRetryDelay(err error, attempt int) time.Duration {
	var retryAfter retryAfterEmbeddingError
	if errors.As(err, &retryAfter) {
		if delay := retryAfter.RetryAfter(); delay > 0 {
			return min(delay, 10*time.Second)
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := embeddingRetryBaseDelay << min(attempt-1, 4)
	if delay > 10*time.Second {
		return 10 * time.Second
	}
	return delay
}

func (r *runState) advanceProgress() {
	r.processedFiles++
	if r.processedFiles > r.totalFiles {
		r.totalFiles = r.processedFiles
	}
	if r.reporter != nil {
		r.reporter.CountProgress("Indexing", r.processedFiles, r.totalFiles)
	}
}

func (r *runState) finalize() error {
	if r.processedFiles > 0 && r.reporter != nil {
		r.reporter.CountProgress("Indexing", r.processedFiles, r.processedFiles)
	}
	if err := r.finishEmbeddingPipeline(); err != nil {
		return err
	}
	if err := r.service.Repo.ActivateSnapshot(r.ctx, r.provider, r.modelID, r.snapshotID); err != nil {
		return fmt.Errorf("activate snapshot: %w", err)
	}
	if err := r.service.Repo.CleanupInactiveSnapshots(r.ctx); err != nil {
		return fmt.Errorf("cleanup inactive snapshots: %w", err)
	}
	if err := r.service.Repo.CleanupUnusedEmbeddings(r.ctx); err != nil {
		return fmt.Errorf("cleanup unused embeddings: %w", err)
	}
	if r.reporter != nil {
		r.reporter.Logf("reused files=%d", r.reusedFiles)
		r.reporter.Logf("reused symbols=%d", r.reusedSymbols)
		r.reporter.Logf("indexing completed")
	}
	return nil
}
