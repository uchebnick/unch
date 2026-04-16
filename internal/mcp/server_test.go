package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type fakeService struct{}

func (fakeService) Version() string { return "vtest" }

func (fakeService) WorkspaceStatus(context.Context) (WorkspaceStatusResult, error) {
	return WorkspaceStatusResult{
		Root:           "/repo",
		StateDir:       "/repo/.semsearch",
		IndexDB:        "/repo/.semsearch/index.db",
		RequestedModel: "embeddinggemma",
		IndexPresent:   true,
	}, nil
}

func (fakeService) SearchCode(_ context.Context, params SearchCodeParams) (SearchCodeResult, error) {
	return SearchCodeResult{
		Query:       params.Query,
		Mode:        fallbackString(params.Mode, "auto"),
		ModelID:     "embeddinggemma",
		StateDir:    "/repo/.semsearch",
		ResultCount: 1,
		Hits: []SearchHit{{
			Path:   "internal/cli/root.go",
			Line:   12,
			Metric: "0.1234",
			Kind:   "function",
			Name:   "Run",
		}},
	}, nil
}

func (fakeService) IndexRepository(context.Context, IndexRepositoryParams) (IndexRepositoryResult, error) {
	return IndexRepositoryResult{
		ModelID:        "embeddinggemma",
		StateDir:       "/repo/.semsearch",
		Version:        4,
		IndexedFiles:   7,
		IndexedSymbols: 42,
	}, nil
}

func TestServerInitializeAndToolsList(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": latestKnownProtocol,
		},
	})
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	reader := bufio.NewReader(output)
	first := readTestFrame(t, reader)
	if first["jsonrpc"] != "2.0" {
		t.Fatalf("initialize response jsonrpc = %v", first["jsonrpc"])
	}
	result := first["result"].(map[string]any)
	if got := result["protocolVersion"]; got != latestKnownProtocol {
		t.Fatalf("protocolVersion = %v, want %s", got, latestKnownProtocol)
	}
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["resources"]; !ok {
		t.Fatalf("initialize capabilities missing resources: %#v", capabilities)
	}

	second := readTestFrame(t, reader)
	tools := second["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3", len(tools))
	}
}

func TestServerBatchInitializeAndToolsList(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	payload, err := json.Marshal([]map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": latestKnownProtocol,
			},
		},
		{
			"jsonrpc": "2.0",
			"method":  "notifications/initialized",
			"params":  map[string]any{},
		},
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
			"params":  map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("marshal batch request: %v", err)
	}
	if err := writeNewlineMessage(input, payload); err != nil {
		t.Fatalf("write batch message: %v", err)
	}

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	raw := readRawFrame(t, bufio.NewReader(output))
	var responses []map[string]any
	if err := json.Unmarshal(raw, &responses); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("batch response count = %d, want 2", len(responses))
	}

	first := responses[0]["result"].(map[string]any)
	if got := first["protocolVersion"]; got != latestKnownProtocol {
		t.Fatalf("protocolVersion = %v, want %s", got, latestKnownProtocol)
	}

	second := responses[1]["result"].(map[string]any)
	tools := second["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3", len(tools))
	}
}

func TestServerInitializeFallsBackToLatestKnownProtocol(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2026-04-01",
		},
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	resp := readTestFrame(t, bufio.NewReader(output))
	result := resp["result"].(map[string]any)
	if got := result["protocolVersion"]; got != latestKnownProtocol {
		t.Fatalf("protocolVersion = %v, want %s", got, latestKnownProtocol)
	}
}

func TestServerSupportsLegacyContentLengthMessages(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeLegacyTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": rootsProtocol,
		},
	})
	writeLegacyTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	reader := bufio.NewReader(output)
	first := readLegacyTestFrame(t, reader)
	if got := first["result"].(map[string]any)["protocolVersion"]; got != rootsProtocol {
		t.Fatalf("protocolVersion = %v, want %s", got, rootsProtocol)
	}

	second := readLegacyTestFrame(t, reader)
	tools := second["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3", len(tools))
	}
}

func TestServerSearchToolCall(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      "search-1",
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_code",
			"arguments": map[string]any{
				"query":   "run cli",
				"details": true,
			},
		},
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	resp := readTestFrame(t, bufio.NewReader(output))
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Found 1 matches") {
		t.Fatalf("search_code text = %q", text)
	}
	structured := result["structuredContent"].(map[string]any)
	if got := structured["query"]; got != "run cli" {
		t.Fatalf("structuredContent.query = %v", got)
	}
}

func TestServerResourcesListAndTemplatesList(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/list",
	})
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/templates/list",
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	reader := bufio.NewReader(output)
	first := readTestFrame(t, reader)
	resources := first["result"].(map[string]any)["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("resources/list returned %d resources, want 2", len(resources))
	}
	firstResource := resources[0].(map[string]any)
	if got := firstResource["uri"]; got != docsOverviewResourceURI {
		t.Fatalf("first resource uri = %v, want %s", got, docsOverviewResourceURI)
	}

	second := readTestFrame(t, reader)
	templates := second["result"].(map[string]any)["resourceTemplates"].([]any)
	if len(templates) != 1 {
		t.Fatalf("resources/templates/list returned %d templates, want 1", len(templates))
	}
	firstTemplate := templates[0].(map[string]any)
	if got := firstTemplate["uriTemplate"]; got != toolResourceTemplateURI {
		t.Fatalf("template uri = %v, want %s", got, toolResourceTemplateURI)
	}
}

func TestServerResourcesReadOverviewAndToolDoc(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]any{
			"uri": workspaceOverviewResourceURI,
		},
	})
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/read",
		"params": map[string]any{
			"uri": "unch://tool/search_code",
		},
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	reader := bufio.NewReader(output)
	first := readTestFrame(t, reader)
	firstContents := first["result"].(map[string]any)["contents"].([]any)
	overview := firstContents[0].(map[string]any)["text"].(string)
	if !strings.Contains(overview, "/repo") {
		t.Fatalf("workspace overview text missing root: %q", overview)
	}
	if !strings.Contains(overview, "unch://tool/search_code") {
		t.Fatalf("workspace overview missing tool doc reference: %q", overview)
	}

	second := readTestFrame(t, reader)
	secondContents := second["result"].(map[string]any)["contents"].([]any)
	toolDoc := secondContents[0].(map[string]any)["text"].(string)
	if !strings.Contains(toolDoc, "`query`") {
		t.Fatalf("tool doc missing query argument: %q", toolDoc)
	}
}

func TestServerInvalidToolArgumentsReturnJSONRPCError(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "search_code",
			"arguments": "not-an-object",
		},
	})

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}

	resp := readTestFrame(t, bufio.NewReader(output))
	errObj := resp["error"].(map[string]any)
	if code := int(errObj["code"].(float64)); code != errCodeInvalidParams {
		t.Fatalf("error.code = %d, want %d", code, errCodeInvalidParams)
	}
}

func writeTestFrame(t *testing.T, w io.Writer, body any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal test frame: %v", err)
	}
	if err := writeNewlineMessage(w, payload); err != nil {
		t.Fatalf("writeNewlineMessage() error: %v", err)
	}
}

func writeLegacyTestFrame(t *testing.T, w io.Writer, body any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal legacy test frame: %v", err)
	}
	if err := writeContentLengthMessage(w, payload); err != nil {
		t.Fatalf("writeContentLengthMessage() error: %v", err)
	}
}

func readTestFrame(t *testing.T, r *bufio.Reader) map[string]any {
	t.Helper()
	payload := readRawFrame(t, r)
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	return decoded
}

func readRawFrame(t *testing.T, r *bufio.Reader) []byte {
	t.Helper()
	payload, err := readNewlineMessage(r)
	if err != nil {
		t.Fatalf("readNewlineMessage() error: %v", err)
	}
	return payload
}

func readLegacyTestFrame(t *testing.T, r *bufio.Reader) map[string]any {
	t.Helper()
	payload, err := readContentLengthMessage(r, "")
	if err != nil {
		t.Fatalf("readContentLengthMessage() error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal legacy frame: %v", err)
	}
	return decoded
}
