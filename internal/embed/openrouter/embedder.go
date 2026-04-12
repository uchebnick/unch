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

type Config struct {
	ModelID     string
	APIKey      string
	BaseURL     string
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
	formatter   appembed.Formatter
	dim         int
}

type embeddingsRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
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
		client = &http.Client{Timeout: 60 * time.Second}
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

func (e *Embedder) embedInput(ctx context.Context, input string) ([]float32, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	reqBody, err := json.Marshal(embeddingsRequest{
		Model:          e.modelID,
		Input:          []string{input},
		EncodingFormat: "float",
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
		return nil, fmt.Errorf("request OpenRouter embeddings: %w", err)
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
		if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
			return nil, fmt.Errorf("OpenRouter embeddings request failed: %s", strings.TrimSpace(payload.Error.Message))
		}
		return nil, fmt.Errorf("OpenRouter embeddings request failed: status %s", resp.Status)
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, fmt.Errorf("OpenRouter embeddings request failed: %s", strings.TrimSpace(payload.Error.Message))
	}
	if len(payload.Data) == 0 || len(payload.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("OpenRouter embeddings response did not contain an embedding")
	}
	if e.dim > 0 && len(payload.Data[0].Embedding) != e.dim {
		return nil, fmt.Errorf("unexpected OpenRouter embedding dimension: got=%d want=%d", len(payload.Data[0].Embedding), e.dim)
	}

	vector := make([]float32, len(payload.Data[0].Embedding))
	copy(vector, payload.Data[0].Embedding)
	return vector, nil
}
