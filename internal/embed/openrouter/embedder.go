package openrouterembed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	appembed "github.com/uchebnick/unch/internal/embed"
	"github.com/uchebnick/unch/internal/indexing"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"
const defaultBatchSize = 128

type Config struct {
	ModelID     string
	APIKey      string
	BaseURL     string
	Dimensions  int
	HTTPReferer string
	AppTitle    string
	HTTPClient  *http.Client
	Formatter   appembed.Formatter
}

type Embedder struct {
	client      *http.Client
	endpoint    string
	apiKey      string
	httpReferer string
	appTitle    string
	modelID     string
	dimensions  int
	formatter   appembed.Formatter
	dim         int
}

type embeddingsRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	Dimensions     int      `json:"dimensions,omitempty"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type requestError struct {
	statusCode int
	status     string
	message    string
	retryAfter time.Duration
	cause      error
}

func (e requestError) Error() string {
	if e.cause != nil {
		return "request OpenRouter embeddings: " + e.cause.Error()
	}
	message := strings.TrimSpace(e.message)
	if message != "" {
		return "OpenRouter embeddings request failed: " + message
	}
	if e.status != "" {
		return "OpenRouter embeddings request failed: status " + e.status
	}
	return "OpenRouter embeddings request failed"
}

func (e requestError) Unwrap() error {
	return e.cause
}

func (e requestError) Temporary() bool {
	return e.cause != nil || e.statusCode == http.StatusTooManyRequests || e.statusCode >= 500
}

func (e requestError) RetryAfter() time.Duration {
	return e.retryAfter
}

func (e requestError) SplitBatch() bool {
	message := strings.ToLower(e.message)
	return e.statusCode == http.StatusRequestEntityTooLarge ||
		e.statusCode == http.StatusBadRequest && strings.Contains(message, "too") ||
		strings.Contains(message, "maximum") ||
		strings.Contains(message, "context length") ||
		strings.Contains(message, "token")
}

func New(ctx context.Context, cfg Config) (*Embedder, error) {
	modelID := strings.TrimSpace(cfg.ModelID)
	if modelID == "" {
		return nil, fmt.Errorf("empty OpenRouter model id")
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	formatter := cfg.Formatter
	if formatter == nil {
		formatter = appembed.FormatterForModel(modelID)
	}

	embedder := &Embedder{
		client:      client,
		endpoint:    baseURL + "/embeddings",
		apiKey:      apiKey,
		httpReferer: strings.TrimSpace(cfg.HTTPReferer),
		appTitle:    strings.TrimSpace(cfg.AppTitle),
		modelID:     modelID,
		dimensions:  cfg.Dimensions,
		formatter:   formatter,
	}

	probe, err := embedder.embedInput(ctx, "unch dimension probe")
	if err != nil {
		return nil, err
	}
	embedder.dim = len(probe)
	if embedder.dim <= 0 {
		return nil, fmt.Errorf("OpenRouter returned empty embedding for %s", modelID)
	}

	return embedder, nil
}

func (e *Embedder) Close() {}

func (e *Embedder) Dim() int {
	if e == nil {
		return 0
	}
	return e.dim
}

func (e *Embedder) EmbedQuery(text string) ([]float32, error) {
	return e.embedInput(context.Background(), e.formatter.FormatQuery(text))
}

func (e *Embedder) IndexedSymbolHash(path string, symbol indexing.IndexedSymbol) string {
	return appembed.IndexedSymbolHash(e.formatter, path, symbol)
}

func (e *Embedder) EmbedIndexedSymbol(path string, symbol indexing.IndexedSymbol) ([]float32, error) {
	return e.embedInput(context.Background(), e.formatter.FormatIndexedSymbolDocument(path, symbol))
}

func (e *Embedder) EmbedIndexedSymbolBatch(items []indexing.EmbedItem) ([][]float32, error) {
	inputs := make([]string, 0, len(items))
	for _, item := range items {
		input := strings.TrimSpace(e.formatter.FormatIndexedSymbolDocument(item.Path, item.Symbol))
		if input == "" {
			inputs = append(inputs, "")
			continue
		}
		inputs = append(inputs, input)
	}
	return e.embedInputsInBatches(inputs)
}

func (e *Embedder) embedInputsInBatches(inputs []string) ([][]float32, error) {
	vectors := make([][]float32, len(inputs))
	for start := 0; start < len(inputs); start += defaultBatchSize {
		end := start + defaultBatchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]
		nonEmptyInputs := make([]string, 0, len(batch))
		nonEmptyIndexes := make([]int, 0, len(batch))
		for i, input := range batch {
			if input == "" {
				continue
			}
			nonEmptyIndexes = append(nonEmptyIndexes, start+i)
			nonEmptyInputs = append(nonEmptyInputs, input)
		}
		if len(nonEmptyInputs) == 0 {
			continue
		}
		batchVectors, err := e.embedInputs(context.Background(), nonEmptyInputs)
		if err != nil {
			return nil, err
		}
		if len(batchVectors) != len(nonEmptyInputs) {
			return nil, fmt.Errorf("OpenRouter returned %d embeddings for %d inputs", len(batchVectors), len(nonEmptyInputs))
		}
		for i, vec := range batchVectors {
			vectors[nonEmptyIndexes[i]] = vec
		}
	}
	return vectors, nil
}

func (e *Embedder) embedInput(ctx context.Context, input string) ([]float32, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	vectors, err := e.embedInputs(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *Embedder) embedInputs(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	reqBody, err := json.Marshal(embeddingsRequest{
		Model:          e.modelID,
		Input:          inputs,
		EncodingFormat: "float",
		Dimensions:     e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("encode embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if e.httpReferer != "" {
		req.Header.Set("HTTP-Referer", e.httpReferer)
	}
	if e.appTitle != "" {
		req.Header.Set("X-Title", e.appTitle)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, requestError{cause: err}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read OpenRouter embeddings response: %w", err)
	}

	var payload embeddingsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter embeddings response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := ""
		if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
			message = strings.TrimSpace(payload.Error.Message)
		}
		return nil, requestError{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			message:    message,
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, fmt.Errorf("OpenRouter embeddings request failed: %s", strings.TrimSpace(payload.Error.Message))
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("OpenRouter embeddings response did not contain an embedding")
	}

	vectors := make([][]float32, len(inputs))
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= len(inputs) {
			return nil, fmt.Errorf("OpenRouter returned embedding with out-of-range index %d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("OpenRouter embeddings response contained an empty embedding")
		}
		if e.dim > 0 && len(item.Embedding) != e.dim {
			return nil, fmt.Errorf("unexpected OpenRouter embedding dimension: got=%d want=%d", len(item.Embedding), e.dim)
		}
		vector := make([]float32, len(item.Embedding))
		copy(vector, item.Embedding)
		vectors[item.Index] = vector
	}
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("OpenRouter embeddings response missing embedding for input %d", i)
		}
	}

	return vectors, nil
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		return seconds
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
