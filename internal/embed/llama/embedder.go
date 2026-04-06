package llamaembed

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jupiterrider/ffi"
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
	mu      sync.Mutex
	model   llama.Model
	ctx     llama.Context
	vocab   llama.Vocab
	dim     int
	profile embeddingBehavior
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

	profile := behaviorForPath(cfg.ModelPath)

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
		cfg.Pooling = profile.DefaultPooling()
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

	return &Embedder{
		model:   model,
		ctx:     ctx,
		vocab:   llama.ModelGetVocab(model),
		dim:     int(llama.ModelNEmbd(model)),
		profile: profile,
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
	if e == nil {
		return nil, fmt.Errorf("nil embedder")
	}

	text = normalizeText(text)
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	tokens := llama.Tokenize(e.vocab, text, true, true)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("tokenize returned zero tokens")
	}

	batch := llama.BatchGetOne(tokens)

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
	return e.Embed(e.profile.FormatQuery(text))
}

// IndexedSymbolHash returns the stable embedding-document hash for one symbol without running the model.
func (e *Embedder) IndexedSymbolHash(path string, symbol indexing.IndexedSymbol) string {
	return hashIndexedSymbolDocument(e.profile, path, symbol)
}

// EmbedIndexedSymbol builds a retrieval document for a symbol and returns its embedding vector.
func (e *Embedder) EmbedIndexedSymbol(path string, symbol indexing.IndexedSymbol) ([]float32, error) {
	return e.Embed(indexedSymbolDocument(e.profile, path, symbol))
}

func hashComment(text string) string {
	sum := xxhash.Sum64String(normalizeText(text))

	var b [8]byte
	b[0] = byte(sum >> 56)
	b[1] = byte(sum >> 48)
	b[2] = byte(sum >> 40)
	b[3] = byte(sum >> 32)
	b[4] = byte(sum >> 24)
	b[5] = byte(sum >> 16)
	b[6] = byte(sum >> 8)
	b[7] = byte(sum)

	return hex.EncodeToString(b[:])
}

func hashIndexedSymbolDocument(profile embeddingBehavior, path string, symbol indexing.IndexedSymbol) string {
	return hashComment("embedding_doc_format:" + profile.ProfileRevision() + "\n" + indexedSymbolDocument(profile, path, symbol))
}

func indexedSymbolDocument(profile embeddingBehavior, path string, symbol indexing.IndexedSymbol) string {
	return profile.FormatIndexedSymbolDocument(path, symbol)
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
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
