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

func (fakeService) WorkspaceStatus(_ context.Context, params WorkspaceStatusParams) (WorkspaceStatusResult, error) {
	root := "/repo"
	if strings.TrimSpace(params.Directory) != "" {
		root = params.Directory
	}
	return WorkspaceStatusResult{
		Root:              root,
		StateDir:          root + "/.semsearch",
		IndexDB:           root + "/.semsearch/index.db",
		RequestedProvider: "llama.cpp",
		RequestedModel:    "embeddinggemma",
		IndexPresent:      true,
		ManifestVersion:   1,
		ManifestSource:    "local",
		Provider:          "llama.cpp",
		ModelID:           "embeddinggemma",
	}, nil
}

func (fakeService) SearchCode(_ context.Context, params SearchCodeParams) (SearchCodeResult, error) {
	return SearchCodeResult{
		Query:       params.Query,
		Mode:        fallbackString(params.Mode, "auto"),
		Provider:    "llama.cpp",
		ModelID:     "embeddinggemma",
		StateDir:    "/repo/.semsearch",
		ResultCount: 1,
		Hits: []SearchHit{{
			Path:          "internal/cli/root.go",
			Line:          12,
			Metric:        "0.1234",
			Kind:          "function",
			Name:          "Run",
			QualifiedName: "Run",
			Signature:     "func Run(...)",
		}},
	}, nil
}

func (fakeService) IndexRepository(context.Context, IndexRepositoryParams) (IndexRepositoryResult, error) {
	return IndexRepositoryResult{
		Provider:       "llama.cpp",
		ModelID:        "embeddinggemma",
		StateDir:       "/repo/.semsearch",
		Version:        4,
		IndexedFiles:   7,
		IndexedSymbols: 42,
	}, nil
}

func TestServerInitialize(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": latestKnownProtocol,
		},
	})

	result := resp["result"].(map[string]any)
	if got := result["protocolVersion"]; got != latestKnownProtocol {
		t.Fatalf("protocolVersion = %v, want %s", got, latestKnownProtocol)
	}
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("initialize capabilities missing tools: %#v", capabilities)
	}
	if _, ok := capabilities["prompts"]; !ok {
		t.Fatalf("initialize capabilities missing prompts: %#v", capabilities)
	}
	if _, ok := capabilities["resources"]; ok {
		t.Fatalf("initialize capabilities unexpectedly include resources: %#v", capabilities)
	}
	instructions := result["instructions"].(string)
	for _, want := range []string{"Call workspace_status first", "Before reading many files", "Do not call index_repository repeatedly", "Provider/model snapshots are isolated"} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("initialize instructions missing %q: %q", want, instructions)
		}
	}
}

func TestServerPing(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "ping-1",
		"method":  "ping",
	})

	if _, ok := resp["result"].(map[string]any); !ok {
		t.Fatalf("ping result = %#v, want object", resp["result"])
	}
}

func TestServerToolsList(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})

	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, name := range []string{"workspace_status", "search_code", "index_repository"} {
		if !names[name] {
			t.Fatalf("tools/list missing %q in %#v", name, names)
		}
	}
	descriptions := map[string]string{}
	for _, tool := range tools {
		item := tool.(map[string]any)
		descriptions[item["name"].(string)] = item["description"].(string)
	}
	if !strings.Contains(descriptions["workspace_status"], "Call this first") {
		t.Fatalf("workspace_status description = %q", descriptions["workspace_status"])
	}
	if !strings.Contains(descriptions["search_code"], "before opening many files") {
		t.Fatalf("search_code description = %q", descriptions["search_code"])
	}
	if !strings.Contains(descriptions["index_repository"], "Avoid repeated rebuilds") {
		t.Fatalf("index_repository description = %q", descriptions["index_repository"])
	}
}

func TestServerPromptsList(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/list",
	})

	prompts := resp["result"].(map[string]any)["prompts"].([]any)
	if len(prompts) != 1 {
		t.Fatalf("prompts/list returned %d prompts, want 1", len(prompts))
	}
	prompt := prompts[0].(map[string]any)
	if got := prompt["name"]; got != "unch" {
		t.Fatalf("prompt name = %v, want unch", got)
	}
	if !strings.Contains(prompt["description"].(string), "semantic code search") {
		t.Fatalf("prompt description = %q", prompt["description"])
	}
}

func TestServerPromptsGetUnch(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]any{
			"name": "unch",
			"arguments": map[string]string{
				"query": "request middleware",
			},
		},
	})

	result := resp["result"].(map[string]any)
	messages := result["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	content := messages[0].(map[string]any)["content"].(map[string]any)
	text := content["text"].(string)
	for _, want := range []string{"workspace_status", "index_repository once", "search_code", "request middleware"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt text missing %q: %q", want, text)
		}
	}
}

func TestServerWorkspaceStatusToolCall(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, toolCall("workspace_status", map[string]any{
		"directory": "/repo",
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "root: /repo") {
		t.Fatalf("workspace_status text = %q", text)
	}
	structured := result["structuredContent"].(map[string]any)
	if got := structured["provider"]; got != "llama.cpp" {
		t.Fatalf("provider = %v, want llama.cpp", got)
	}
}

func TestServerSearchCodeToolCall(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, toolCall("search_code", map[string]any{
		"query":   "run cli",
		"details": true,
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Found 1 matches") {
		t.Fatalf("search_code text = %q", text)
	}
	structured := result["structuredContent"].(map[string]any)
	if got := structured["query"]; got != "run cli" {
		t.Fatalf("structuredContent.query = %v", got)
	}
}

func TestServerIndexRepositoryToolCall(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, toolCall("index_repository", map[string]any{
		"excludes": []string{"node_modules", "dist"},
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Indexed 42 symbols in 7 files.") {
		t.Fatalf("index_repository text = %q", text)
	}
	structured := result["structuredContent"].(map[string]any)
	if got := int(structured["indexed_symbols"].(float64)); got != 42 {
		t.Fatalf("indexed_symbols = %d, want 42", got)
	}
}

func TestServerInvalidJSON(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	if err := writeContentLengthMessage(input, []byte("{bad")); err != nil {
		t.Fatalf("writeContentLengthMessage() error: %v", err)
	}
	output := serveRaw(t, input)
	resp := readTestFrame(t, bufio.NewReader(output))
	assertRPCError(t, resp, errCodeParse)
}

func TestServerInvalidParams(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, toolCall("search_code", "not-an-object"))
	assertRPCError(t, resp, errCodeInvalidParams)
}

func TestServerUnknownMethod(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/list",
	})
	assertRPCError(t, resp, errCodeMethodMissing)
}

func TestServerUnknownTool(t *testing.T) {
	t.Parallel()

	resp := serveOne(t, toolCall("missing_tool", map[string]any{}))
	result := resp["result"].(map[string]any)
	if isError, ok := result["isError"].(bool); !ok || !isError {
		t.Fatalf("isError = %v, want true", result["isError"])
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "unknown tool") {
		t.Fatalf("unknown tool text = %q", text)
	}
}

func TestServerNotificationIgnored(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	writeTestFrame(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "ping",
	})

	output := serveRaw(t, input)
	reader := bufio.NewReader(output)
	resp := readTestFrame(t, reader)
	if got := int(resp["id"].(float64)); got != 2 {
		t.Fatalf("response id = %d, want 2", got)
	}
	if _, err := readContentLengthMessage(reader); err != io.EOF {
		t.Fatalf("second read error = %v, want EOF", err)
	}
}

func TestServerJSONLineFraming(t *testing.T) {
	t.Parallel()

	input := bytes.NewBuffer(nil)
	writeTestLine(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": latestKnownProtocol,
			"capabilities": map[string]any{
				"elicitation": map[string]any{"form": map[string]any{}},
			},
			"clientInfo": map[string]any{
				"name":    "codex-mcp-client",
				"version": "0.125.0",
			},
		},
	})
	writeTestLine(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})

	output := serveRaw(t, input)
	reader := bufio.NewReader(output)
	initResp := readTestLine(t, reader)
	if got := int(initResp["id"].(float64)); got != 0 {
		t.Fatalf("initialize response id = %d, want 0", got)
	}
	toolsResp := readTestLine(t, reader)
	tools := toolsResp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools/list returned %d tools, want 3", len(tools))
	}
}

func serveOne(t *testing.T, request map[string]any) map[string]any {
	t.Helper()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, request)
	output := serveRaw(t, input)
	return readTestFrame(t, bufio.NewReader(output))
}

func serveRaw(t *testing.T, input *bytes.Buffer) *bytes.Buffer {
	t.Helper()

	output := bytes.NewBuffer(nil)
	server := NewServer(fakeService{})
	if err := server.Serve(context.Background(), input, output); err != nil {
		t.Fatalf("Serve() error: %v", err)
	}
	return output
}

func toolCall(name string, args any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      "tool-1",
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

func writeTestFrame(t *testing.T, w io.Writer, body any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal test frame: %v", err)
	}
	if err := writeContentLengthMessage(w, payload); err != nil {
		t.Fatalf("writeContentLengthMessage() error: %v", err)
	}
}

func writeTestLine(t *testing.T, w io.Writer, body any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal test line: %v", err)
	}
	if _, err := w.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write test line: %v", err)
	}
}

func readTestFrame(t *testing.T, r *bufio.Reader) map[string]any {
	t.Helper()
	payload, err := readContentLengthMessage(r)
	if err != nil {
		t.Fatalf("readContentLengthMessage() error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	return decoded
}

func readTestLine(t *testing.T, r *bufio.Reader) map[string]any {
	t.Helper()
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read test line: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(line), &decoded); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	return decoded
}

func assertRPCError(t *testing.T, resp map[string]any, wantCode int) {
	t.Helper()
	errObj := resp["error"].(map[string]any)
	if code := int(errObj["code"].(float64)); code != wantCode {
		t.Fatalf("error.code = %d, want %d", code, wantCode)
	}
}
