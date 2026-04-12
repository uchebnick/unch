package llamaembed

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jupiterrider/ffi"
	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/indexing"
	unchruntime "github.com/uchebnick/unch/internal/runtime"
)

type Config struct {
	ModelPath   string
	LibPath     string
	ContextSize int
	Verbose     bool
	Pooling     llama.PoolingType
}

type Embedder struct {
	mu          sync.Mutex
	model       llama.Model
	ctx         llama.Context
	vocab       llama.Vocab
	dim         int
	formatter   appembed.Formatter
	contextSize int
	tokenLimit  int
}

var (
	llamaGlobalMu       sync.Mutex
	llamaLoaded         bool
	llamaLoadedLibPath  string
	llamaInitRefCounter int
	preloadedYzmaLibs   []ffi.Lib
)

// New loads the yzma runtime, opens the GGUF model, and prepares an embedding context.
func New(cfg Config) (*Embedder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	formatter := formatterForPath(cfg.ModelPath)

	resolvedLibPath, _, err := unchruntime.ResolveYzmaLibPath(cfg.LibPath)
	if err != nil {
		return nil, err
	}
	cfg.LibPath = resolvedLibPath

	if err := unchruntime.EnsureDynamicLibraryLookupPath(cfg.LibPath); err != nil {
		return nil, fmt.Errorf("prepare dynamic library lookup path: %w", err)
	}

	if cfg.ContextSize <= 0 {
		cfg.ContextSize = profileForPath(cfg.ModelPath).DefaultContextSize
	}
	if cfg.Pooling == 0 {
		cfg.Pooling = DefaultPoolingForModelPath(cfg.ModelPath)
	}

	llamaGlobalMu.Lock()
	defer llamaGlobalMu.Unlock()

	if !llamaLoaded {
		if err := preloadYzmaSharedLibraries(cfg.LibPath); err != nil {
			return nil, fmt.Errorf("preload yzma shared libraries: %w", err)
		}
		if err := llama.Load(cfg.LibPath); err != nil {
			return nil, fmt.Errorf("load yzma library: %w", err)
		}
		llamaLoaded = true
		llamaLoadedLibPath = cfg.LibPath
	} else if llamaLoadedLibPath != cfg.LibPath {
		return nil, fmt.Errorf(
			"yzma already loaded from another lib path: loaded=%s requested=%s",
			llamaLoadedLibPath,
			cfg.LibPath,
		)
	}

	if !cfg.Verbose {
		llama.LogSet(llama.LogSilent())
	}

	if llamaInitRefCounter == 0 {
		llama.Init()
	}
	llamaInitRefCounter++

	model, err := llama.ModelLoadFromFile(cfg.ModelPath, llama.ModelDefaultParams())
	if err != nil {
		llamaInitRefCounter--
		if llamaInitRefCounter == 0 {
			llama.Close()
		}
		return nil, fmt.Errorf("load model from file: %w", err)
	}
	if model == 0 {
		llamaInitRefCounter--
		if llamaInitRefCounter == 0 {
			llama.Close()
		}
		return nil, fmt.Errorf("model handle is zero")
	}

	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = uint32(cfg.ContextSize)
	ctxParams.NBatch = uint32(cfg.ContextSize)
	ctxParams.NUbatch = uint32(cfg.ContextSize)
	ctxParams.PoolingType = cfg.Pooling
	ctxParams.Embeddings = 1

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		_ = llama.ModelFree(model)
		llamaInitRefCounter--
		if llamaInitRefCounter == 0 {
			llama.Close()
		}
		return nil, fmt.Errorf("init context from model: %w", err)
	}

	tokenLimit := effectiveTokenLimit(
		cfg.ContextSize,
		int(llama.NCtx(ctx)),
		int(llama.NBatch(ctx)),
		int(llama.NUBatch(ctx)),
	)

	return &Embedder{
		model:       model,
		ctx:         ctx,
		vocab:       llama.ModelGetVocab(model),
		dim:         int(llama.ModelNEmbd(model)),
		formatter:   formatter,
		contextSize: cfg.ContextSize,
		tokenLimit:  tokenLimit,
	}, nil
}

func (c Config) Validate() error {
	if c.ModelPath == "" {
		return fmt.Errorf("empty model path")
	}
	if c.LibPath == "" {
		return fmt.Errorf("empty yzma lib path")
	}
	return nil
}

func (e *Embedder) Close() {
	if e == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx != 0 {
		_ = llama.Free(e.ctx)
		e.ctx = 0
	}
	if e.model != 0 {
		_ = llama.ModelFree(e.model)
		e.model = 0
	}

	llamaGlobalMu.Lock()
	defer llamaGlobalMu.Unlock()

	if llamaInitRefCounter > 0 {
		llamaInitRefCounter--
		if llamaInitRefCounter == 0 {
			llama.Close()
		}
	}
}

func (e *Embedder) Dim() int {
	if e == nil {
		return 0
	}
	return e.dim
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	text = normalizeText(text)

	tokens := llama.Tokenize(e.vocab, text, true, false)
	if len(tokens) == 0 {
		return nil, nil // Return empty if text results in zero tokens
	}

	// Truncate tokens to the smallest safe bound reported by the runtime.
	if len(tokens) > e.tokenLimit {
		tokens = tokens[:e.tokenLimit]
	}

	// Clear memory before processing new tokens
	mem, err := llama.GetMemory(e.ctx)
	if err == nil {
		_ = llama.MemoryClear(mem, true)
	}

	batch := llama.BatchGetOne(tokens)
	// yzma's single-sequence batch is not safe to free here: on real indexing
	// workloads that triggered invalid pointer / heap corruption crashes on both
	// Linux and Windows ARM CI.

	ret, err := llama.Decode(e.ctx, batch)
	if err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}
	if ret != 0 {
		return nil, fmt.Errorf("decode returned non-zero: %d", ret)
	}

	vec, err := llama.GetEmbeddingsSeq(e.ctx, 0, int32(e.dim))
	if err != nil {
		return nil, fmt.Errorf("get embeddings: %w", err)
	}
	if len(vec) != e.dim {
		return nil, fmt.Errorf("unexpected embedding dimension: got=%d want=%d", len(vec), e.dim)
	}

	out := make([]float32, len(vec))
	copy(out, vec)
	l2Normalize(out)
	return out, nil
}

func (e *Embedder) EmbedQuery(text string) ([]float32, error) {
	return e.Embed(e.formatter.FormatQuery(text))
}

// IndexedSymbolHash returns the stable embedding-document hash for one symbol without running the model.
func (e *Embedder) IndexedSymbolHash(path string, symbol indexing.IndexedSymbol) string {
	return appembed.IndexedSymbolHash(e.formatter, path, symbol)
}

// EmbedIndexedSymbol builds a retrieval document for a symbol and returns its embedding vector.
func (e *Embedder) EmbedIndexedSymbol(path string, symbol indexing.IndexedSymbol) ([]float32, error) {
	return e.Embed(e.formatter.FormatIndexedSymbolDocument(path, symbol))
}

func effectiveTokenLimit(requested int, limits ...int) int {
	limit := requested
	for _, candidate := range limits {
		if candidate <= 0 {
			continue
		}
		if limit <= 0 || candidate < limit {
			limit = candidate
		}
	}
	return limit
}

func normalizeText(text string) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func l2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x * x)
	}
	if sum == 0 {
		return
	}

	inv := float32(1.0 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

func preloadYzmaSharedLibraries(libDir string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	filenames, err := darwinPreloadLibraryNames(libDir)
	if err != nil {
		return err
	}

	for _, name := range filenames {
		lib, err := ffi.Load(filepath.Join(libDir, name))
		if err != nil {
			return fmt.Errorf("preload %s: %w", name, err)
		}
		preloadedYzmaLibs = append(preloadedYzmaLibs, lib)
	}

	return nil
}

func darwinPreloadLibraryNames(libDir string) ([]string, error) {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return nil, fmt.Errorf("read yzma lib dir: %w", err)
	}

	var names []string
	add := func(name string) {
		if _, err := os.Stat(filepath.Join(libDir, name)); err == nil {
			names = append(names, name)
		}
	}

	add("libggml-base.dylib")

	var optional []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(name, "libggml-") || !strings.HasSuffix(name, ".dylib") {
			continue
		}
		if name == "libggml-base.dylib" {
			continue
		}
		optional = append(optional, name)
	}
	sort.Strings(optional)
	names = append(names, optional...)

	add("libggml.dylib")
	add("libllama.dylib")

	return names, nil
}
