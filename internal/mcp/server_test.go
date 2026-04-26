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

func (fakeService) WorkspaceStatus(context.Context, WorkspaceStatusParams) (WorkspaceStatusResult, error) {
	return WorkspaceStatusResult{
		Root:              "/repo",
		StateDir:          "/repo/.semsearch",
		IndexDB:           "/repo/.semsearch/index.db",
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

func (fakeService) CreateCIWorkflow(context.Context, CreateCIWorkflowParams) (CreateCIWorkflowResult, error) {
	return CreateCIWorkflowResult{
		Root:         "/repo",
		WorkflowPath: "/repo/.github/workflows/unch-index.yml",
		Created:      true,
	}, nil
}

func (fakeService) BindRemoteCI(context.Context, BindRemoteCIParams) (BindRemoteCIResult, error) {
	return BindRemoteCIResult{
		Root:         "/repo",
		StateDir:     "/repo/.semsearch",
		ManifestPath: "/repo/.semsearch/manifest.json",
		CIURL:        "https://github.com/acme/widgets/actions/workflows/unch-index.yml",
		Version:      1,
	}, nil
}

func (fakeService) RemoteSyncIndex(context.Context, RemoteSyncIndexParams) (RemoteSyncIndexResult, error) {
	return RemoteSyncIndexResult{
		Root:       "/repo",
		StateDir:   "/repo/.semsearch",
		Checked:    true,
		Downloaded: true,
		Version:    2,
		Source:     "remote",
		CIURL:      "https://github.com/acme/widgets/actions/workflows/unch-index.yml",
		Note:       "Downloaded remote index version 2",
	}, nil
}

func (fakeService) RemoteDownloadIndex(context.Context, RemoteDownloadIndexParams) (RemoteDownloadIndexResult, error) {
	return RemoteDownloadIndexResult{
		Root:       "/repo",
		StateDir:   "/repo/.semsearch",
		Downloaded: true,
		CommitSHA:  "abc123",
		Version:    3,
		Source:     "local",
		Note:       "Downloaded search index artifact for abc123 from workflow run 42",
	}, nil
}

type capturingService struct {
	fakeService
	workspaceParams      WorkspaceStatusParams
	searchParams         SearchCodeParams
	indexParams          IndexRepositoryParams
	createCIParams       CreateCIWorkflowParams
	bindCIParams         BindRemoteCIParams
	remoteSyncParams     RemoteSyncIndexParams
	remoteDownloadParams RemoteDownloadIndexParams
}

func (s *capturingService) WorkspaceStatus(ctx context.Context, params WorkspaceStatusParams) (WorkspaceStatusResult, error) {
	s.workspaceParams = params
	return s.fakeService.WorkspaceStatus(ctx, params)
}

func (s *capturingService) SearchCode(ctx context.Context, params SearchCodeParams) (SearchCodeResult, error) {
	s.searchParams = params
	return s.fakeService.SearchCode(ctx, params)
}

func (s *capturingService) IndexRepository(ctx context.Context, params IndexRepositoryParams) (IndexRepositoryResult, error) {
	s.indexParams = params
	return s.fakeService.IndexRepository(ctx, params)
}

func (s *capturingService) CreateCIWorkflow(ctx context.Context, params CreateCIWorkflowParams) (CreateCIWorkflowResult, error) {
	s.createCIParams = params
	return s.fakeService.CreateCIWorkflow(ctx, params)
}

func (s *capturingService) BindRemoteCI(ctx context.Context, params BindRemoteCIParams) (BindRemoteCIResult, error) {
	s.bindCIParams = params
	return s.fakeService.BindRemoteCI(ctx, params)
}

func (s *capturingService) RemoteSyncIndex(ctx context.Context, params RemoteSyncIndexParams) (RemoteSyncIndexResult, error) {
	s.remoteSyncParams = params
	return s.fakeService.RemoteSyncIndex(ctx, params)
}

func (s *capturingService) RemoteDownloadIndex(ctx context.Context, params RemoteDownloadIndexParams) (RemoteDownloadIndexResult, error) {
	s.remoteDownloadParams = params
	return s.fakeService.RemoteDownloadIndex(ctx, params)
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
	if len(tools) != 7 {
		t.Fatalf("tools/list returned %d tools, want 7", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, name := range []string{"workspace_status", "search_code", "index_repository", "create_ci_workflow", "bind_remote_ci", "remote_sync_index", "remote_download_index"} {
		if !names[name] {
			t.Fatalf("tools/list missing %q in %#v", name, names)
		}
	}
	descriptions := map[string]string{}
	for _, tool := range tools {
		item := tool.(map[string]any)
		descriptions[item["name"].(string)] = item["description"].(string)
		properties := item["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if _, ok := properties["directory"]; !ok {
			t.Fatalf("%s schema missing directory property", item["name"])
		}
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
	if !strings.Contains(descriptions["remote_sync_index"], "bound remote") {
		t.Fatalf("remote_sync_index description = %q", descriptions["remote_sync_index"])
	}
}

func TestServerWorkspaceStatusToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("workspace_status", map[string]any{
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
	if got := service.workspaceParams.Directory; got != "/repo" {
		t.Fatalf("workspace_status directory = %q, want /repo", got)
	}
}

func TestServerSearchCodeToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("search_code", map[string]any{
		"directory": "/repo",
		"query":     "run cli",
		"details":   true,
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
	if got := service.searchParams.Directory; got != "/repo" {
		t.Fatalf("search_code directory = %q, want /repo", got)
	}
}

func TestServerIndexRepositoryToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("index_repository", map[string]any{
		"directory": "/repo",
		"excludes":  []string{"node_modules", "dist"},
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
	if got := service.indexParams.Directory; got != "/repo" {
		t.Fatalf("index_repository directory = %q, want /repo", got)
	}
}

func TestServerCreateCIWorkflowToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("create_ci_workflow", map[string]any{
		"directory": "/repo",
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Created /repo/.github/workflows/unch-index.yml") {
		t.Fatalf("create_ci_workflow text = %q", text)
	}
	if got := service.createCIParams.Directory; got != "/repo" {
		t.Fatalf("create_ci_workflow directory = %q, want /repo", got)
	}
}

func TestServerBindRemoteCIToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("bind_remote_ci", map[string]any{
		"directory": "/repo",
		"target":    "https://github.com/acme/widgets",
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Bound /repo/.semsearch/manifest.json") {
		t.Fatalf("bind_remote_ci text = %q", text)
	}
	if got := service.bindCIParams.Target; got != "https://github.com/acme/widgets" {
		t.Fatalf("bind_remote_ci target = %q, want repo URL", got)
	}
}

func TestServerRemoteSyncIndexToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("remote_sync_index", map[string]any{
		"directory":     "/repo",
		"allow_missing": true,
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "downloaded: true") {
		t.Fatalf("remote_sync_index text = %q", text)
	}
	if !service.remoteSyncParams.AllowMissing {
		t.Fatalf("remote_sync_index allow_missing = false, want true")
	}
}

func TestServerRemoteDownloadIndexToolCall(t *testing.T) {
	t.Parallel()

	service := &capturingService{}
	resp := serveOneWithService(t, service, toolCall("remote_download_index", map[string]any{
		"directory": "/repo",
		"target":    "https://github.com/acme/widgets",
		"commit":    "abc123",
	}))
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "commit: abc123") {
		t.Fatalf("remote_download_index text = %q", text)
	}
	if got := service.remoteDownloadParams.Commit; got != "abc123" {
		t.Fatalf("remote_download_index commit = %q, want abc123", got)
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

func serveOne(t *testing.T, request map[string]any) map[string]any {
	t.Helper()

	return serveOneWithService(t, fakeService{}, request)
}

func serveOneWithService(t *testing.T, service Service, request map[string]any) map[string]any {
	t.Helper()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, request)
	output := serveRawWithService(t, service, input)
	return readTestFrame(t, bufio.NewReader(output))
}

func serveRaw(t *testing.T, input *bytes.Buffer) *bytes.Buffer {
	t.Helper()

	return serveRawWithService(t, fakeService{}, input)
}

func serveRawWithService(t *testing.T, service Service, input *bytes.Buffer) *bytes.Buffer {
	t.Helper()

	output := bytes.NewBuffer(nil)
	server := NewServer(service)
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

func assertRPCError(t *testing.T, resp map[string]any, wantCode int) {
	t.Helper()
	errObj := resp["error"].(map[string]any)
	if code := int(errObj["code"].(float64)); code != wantCode {
		t.Fatalf("error.code = %d, want %d", code, wantCode)
	}
}
