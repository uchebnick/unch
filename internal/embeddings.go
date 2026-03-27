package internal

// @filectx: Embedding adapter around yzma llama bindings that loads the GGUF model, produces normalized vectors, and hashes indexed comments.

import (
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/hybridgroup/yzma/pkg/llama"
)

type EmbeddingsConfig struct {
	ModelPath   string
	LibPath     string
	ContextSize int
	BatchSize   int
	Verbose     bool
	Pooling     llama.PoolingType
}

type Embedder struct {
	mu    sync.Mutex
	model llama.Model
	ctx   llama.Context
	vocab llama.Vocab
	dim   int
}

var (
	llamaGlobalMu       sync.Mutex
	llamaLoaded         bool
	llamaLoadedLibPath  string
	llamaInitRefCounter int
)

const (
	embeddingGemmaRetrievalQueryPrefix = "task: code retrieval | query: "
	embeddingGemmaDocumentPrefix       = "title: %s | text: %s"
	embeddingDocFormatVersion          = "v3"
)

// @search: NewEmbedder loads yzma shared libraries once per process and rejects switching to a different lib path after llama is initialized.
func NewEmbedder(cfg EmbeddingsConfig) (*Embedder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	resolvedLibPath, _, err := resolveYzmaLibPath(cfg.LibPath)
	if err != nil {
		return nil, err
	}
	cfg.LibPath = resolvedLibPath

	if err := ensureDynamicLibraryLookupPath(cfg.LibPath); err != nil {
		return nil, fmt.Errorf("prepare dynamic library lookup path: %w", err)
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 2048
	}
	if cfg.Pooling == 0 {
		cfg.Pooling = llama.PoolingTypeMean
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
	ctxParams.NBatch = uint32(cfg.BatchSize)
	ctxParams.PoolingType = cfg.Pooling
	ctxParams.Embeddings = 1

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		llama.ModelFree(model)
		llamaInitRefCounter--
		if llamaInitRefCounter == 0 {
			llama.Close()
		}
		return nil, fmt.Errorf("init context from model: %w", err)
	}

	return &Embedder{
		model: model,
		ctx:   ctx,
		vocab: llama.ModelGetVocab(model),
		dim:   int(llama.ModelNEmbd(model)),
	}, nil
}

func (c EmbeddingsConfig) Validate() error {
	if c.ModelPath == "" {
		return fmt.Errorf("empty model path")
	}
	if c.LibPath == "" {
		return fmt.Errorf("empty yzma lib path")
	}
	return nil
}

// @search: Embedder.Close frees model and context handles and closes llama when the last reference is released.
func (e *Embedder) Close() {
	if e == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx != 0 {
		llama.Free(e.ctx)
		e.ctx = 0
	}
	if e.model != 0 {
		llama.ModelFree(e.model)
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

// @search: Embed tokenizes text with the model vocabulary, decodes once, reads the pooled embedding vector, and l2-normalizes the result.
func (e *Embedder) Embed(text string) ([]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("nil embedder")
	}

	text = normalizeCommentText(text)
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
	return e.Embed(formatEmbeddingGemmaQuery(text))
}

func (e *Embedder) EmbedDocument(title string, text string) ([]float32, error) {
	return e.Embed(formatEmbeddingGemmaDocument(title, text))
}

func (e *Embedder) EmbedBatch(texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("nil embedder")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, fmt.Errorf("embed batch item %d: %w", i, err)
		}
		out = append(out, vec)
	}

	return out, nil
}

// @search: HashComment normalizes text and uses xxhash so identical comments reuse stored embeddings across files and indexing versions.
func HashComment(text string) string {
	sum := xxhash.Sum64String(normalizeCommentText(text))

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

func normalizeCommentText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func formatIndexedCommentDocument(path string, comment string, commentContext string, followingText string) string {
	comment = normalizeCommentText(comment)
	commentContext = normalizeCommentText(commentContext)
	followingText = normalizeCommentText(followingText)

	var body strings.Builder
	body.WriteString("Comment: ")
	body.WriteString(comment)
	if commentContext != "" {
		body.WriteString("\nContext: ")
		body.WriteString(commentContext)
	}
	if followingText != "" {
		body.WriteString("\nFollowing code:\n")
		body.WriteString(followingText)
	}

	title := strings.TrimSpace(filepath.Base(path))
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = "none"
	}
	return formatEmbeddingGemmaDocument(title, body.String())
}

func formatEmbeddingGemmaQuery(text string) string {
	text = normalizeCommentText(text)
	return embeddingGemmaRetrievalQueryPrefix + text
}

func formatEmbeddingGemmaDocument(title string, text string) string {
	title = normalizeEmbeddingGemmaTitle(title)
	text = normalizeCommentText(text)
	return fmt.Sprintf(embeddingGemmaDocumentPrefix, title, text)
}

func normalizeEmbeddingGemmaTitle(title string) string {
	title = normalizeCommentText(title)
	title = strings.ReplaceAll(title, "|", "/")
	if title == "" {
		return "none"
	}
	return title
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
