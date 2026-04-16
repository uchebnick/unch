package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	jsonRPCVersion       = "2.0"
	legacyProtocol       = "2024-11-05"
	toolsProtocol        = "2025-03-26"
	rootsProtocol        = "2025-06-18"
	latestKnownProtocol  = "2025-11-25"
	defaultProtocol      = latestKnownProtocol
	errCodeParse         = -32700
	errCodeInvalidReq    = -32600
	errCodeMethodMissing = -32601
	errCodeInvalidParams = -32602
	errCodeInternal      = -32603
	errCodeNotFound      = -32002
)

var supportedProtocols = map[string]struct{}{
	legacyProtocol:      {},
	toolsProtocol:       {},
	rootsProtocol:       {},
	latestKnownProtocol: {},
}

type Service interface {
	Version() string
	WorkspaceStatus(context.Context) (WorkspaceStatusResult, error)
	SearchCode(context.Context, SearchCodeParams) (SearchCodeResult, error)
	IndexRepository(context.Context, IndexRepositoryParams) (IndexRepositoryResult, error)
}

type SearchCodeParams struct {
	Query       string   `json:"query"`
	Limit       int      `json:"limit,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	MaxDistance *float64 `json:"max_distance,omitempty"`
	Details     bool     `json:"details,omitempty"`
}

type SearchHit struct {
	Path          string  `json:"path"`
	Line          int     `json:"line"`
	Metric        string  `json:"metric"`
	Kind          string  `json:"kind,omitempty"`
	Name          string  `json:"name,omitempty"`
	QualifiedName string  `json:"qualified_name,omitempty"`
	Signature     string  `json:"signature,omitempty"`
	Documentation string  `json:"documentation,omitempty"`
	Body          string  `json:"body,omitempty"`
	Distance      float64 `json:"distance,omitempty"`
}

type SearchCodeResult struct {
	Query       string      `json:"query"`
	Mode        string      `json:"mode"`
	Provider    string      `json:"provider,omitempty"`
	ModelID     string      `json:"model_id"`
	StateDir    string      `json:"state_dir"`
	ResultCount int         `json:"result_count"`
	Hits        []SearchHit `json:"hits"`
}

type IndexRepositoryParams struct {
	Excludes      []string `json:"excludes,omitempty"`
	Gitignore     string   `json:"gitignore,omitempty"`
	CommentPrefix string   `json:"comment_prefix,omitempty"`
	ContextPrefix string   `json:"context_prefix,omitempty"`
}

type IndexRepositoryResult struct {
	Provider              string `json:"provider,omitempty"`
	ModelID               string `json:"model_id"`
	StateDir              string `json:"state_dir"`
	Version               int64  `json:"version"`
	IndexedFiles          int    `json:"indexed_files"`
	IndexedSymbols        int    `json:"indexed_symbols"`
	DetachedRemoteBinding bool   `json:"detached_remote_binding,omitempty"`
}

type WorkspaceStatusResult struct {
	Root              string `json:"root"`
	StateDir          string `json:"state_dir"`
	IndexDB           string `json:"index_db"`
	RequestedProvider string `json:"requested_provider,omitempty"`
	RequestedModel    string `json:"requested_model,omitempty"`
	RequestedLib      string `json:"requested_lib,omitempty"`
	ContextSize       int    `json:"ctx_size,omitempty"`
	IndexPresent      bool   `json:"index_present"`
	ManifestVersion   int64  `json:"manifest_version,omitempty"`
	ManifestSource    string `json:"manifest_source,omitempty"`
	RemoteCIURL       string `json:"remote_ci_url,omitempty"`
	Provider          string `json:"provider,omitempty"`
	ModelID           string `json:"model_id,omitempty"`
	ResolvedModel     string `json:"resolved_model,omitempty"`
	ResolvedLib       string `json:"resolved_lib,omitempty"`
}

type Server struct {
	service Service
	writer  io.Writer
	writeMu sync.Mutex
	format  transportFormat
}

type transportFormat int

const (
	transportFormatUnknown transportFormat = iota
	transportFormatNewline
	transportFormatContentLength
)

func NewServer(service Service) *Server {
	return &Server{service: service}
}

func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.writer = w
	reader := bufio.NewReader(r)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		payload, err := s.readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if bytes.HasPrefix(bytes.TrimSpace(payload), []byte("[")) {
			if err := s.handleBatch(ctx, payload); err != nil {
				return err
			}
			continue
		}

		var req requestEnvelope
		if err := json.Unmarshal(payload, &req); err != nil {
			if writeErr := s.writeError(nil, errCodeParse, "failed to decode JSON request"); writeErr != nil {
				return writeErr
			}
			continue
		}

		if strings.TrimSpace(req.JSONRPC) != jsonRPCVersion || strings.TrimSpace(req.Method) == "" {
			if req.ID != nil {
				if err := s.writeError(req.ID, errCodeInvalidReq, "invalid JSON-RPC request"); err != nil {
					return err
				}
			}
			continue
		}

		if req.ID == nil {
			continue
		}

		if err := s.handleRequest(ctx, req); err != nil {
			return err
		}
	}
}

func (s *Server) handleBatch(ctx context.Context, payload []byte) error {
	var batch []json.RawMessage
	if err := json.Unmarshal(payload, &batch); err != nil {
		return s.writeError(nil, errCodeParse, "failed to decode JSON request")
	}
	if len(batch) == 0 {
		return s.writeError(nil, errCodeInvalidReq, "invalid JSON-RPC request")
	}

	responses := make([]responseEnvelope, 0, len(batch))
	for _, item := range batch {
		var req requestEnvelope
		if err := json.Unmarshal(item, &req); err != nil {
			responses = append(responses, s.errorEnvelope(nil, errCodeInvalidReq, "invalid JSON-RPC request"))
			continue
		}
		if strings.TrimSpace(req.JSONRPC) != jsonRPCVersion || strings.TrimSpace(req.Method) == "" {
			responses = append(responses, s.errorEnvelope(req.ID, errCodeInvalidReq, "invalid JSON-RPC request"))
			continue
		}
		if req.ID == nil {
			continue
		}

		responses = append(responses, s.buildResponse(ctx, req))
	}

	if len(responses) == 0 {
		return nil
	}
	return s.writeBatchResponses(responses)
}

func (s *Server) handleRequest(ctx context.Context, req requestEnvelope) error {
	return s.writeEnvelope(s.buildResponse(ctx, req))
}

func (s *Server) buildResponse(ctx context.Context, req requestEnvelope) responseEnvelope {
	switch req.Method {
	case "initialize":
		var params initializeParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return s.errorEnvelope(req.ID, errCodeInvalidParams, "invalid initialize params")
			}
		}
		result := initializeResult{
			ProtocolVersion: negotiatedProtocol(params.ProtocolVersion),
			Capabilities: map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
				"resources": map[string]any{},
			},
			ServerInfo: serverInfo{
				Name:    "unch",
				Version: s.service.Version(),
			},
			Instructions: "Use workspace_status to inspect the configured repository, search_code to retrieve symbol matches, and index_repository to rebuild the local semantic index.",
		}
		return s.resultEnvelope(req.ID, result)
	case "ping":
		return s.resultEnvelope(req.ID, map[string]any{})
	case "tools/list":
		return s.resultEnvelope(req.ID, listToolsResult{Tools: toolDefinitions()})
	case "resources/list":
		return s.resultEnvelope(req.ID, listResourcesResult{Resources: resourceDefinitions()})
	case "resources/templates/list":
		return s.resultEnvelope(req.ID, listResourceTemplatesResult{ResourceTemplates: resourceTemplateDefinitions()})
	case "resources/read":
		var params resourceReadParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorEnvelope(req.ID, errCodeInvalidParams, "invalid resources/read params")
		}
		result, err := s.readResource(ctx, params)
		if err != nil {
			var rpcErr rpcError
			if errors.As(err, &rpcErr) {
				return s.errorEnvelope(req.ID, rpcErr.Code, rpcErr.Message)
			}
			return s.errorEnvelope(req.ID, errCodeInternal, err.Error())
		}
		return s.resultEnvelope(req.ID, result)
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorEnvelope(req.ID, errCodeInvalidParams, "invalid tools/call params")
		}
		result, err := s.callTool(ctx, params)
		if err != nil {
			var rpcErr rpcError
			if errors.As(err, &rpcErr) {
				return s.errorEnvelope(req.ID, rpcErr.Code, rpcErr.Message)
			}
			return s.errorEnvelope(req.ID, errCodeInternal, err.Error())
		}
		return s.resultEnvelope(req.ID, result)
	default:
		return s.errorEnvelope(req.ID, errCodeMethodMissing, fmt.Sprintf("method %q is not supported", req.Method))
	}
}

func (s *Server) readResource(ctx context.Context, params resourceReadParams) (readResourceResult, error) {
	uri := strings.TrimSpace(params.URI)
	if uri == "" {
		return readResourceResult{}, invalidParamsError("resources/read requires a non-empty uri")
	}

	var text string
	switch uri {
	case docsOverviewResourceURI:
		text = renderDocsOverview()
	case workspaceOverviewResourceURI:
		status, err := s.service.WorkspaceStatus(ctx)
		if err != nil {
			return readResourceResult{}, err
		}
		text = renderWorkspaceOverviewResource(status)
	default:
		if strings.HasPrefix(uri, toolResourcePrefix) {
			content, err := renderToolResource(strings.TrimPrefix(uri, toolResourcePrefix))
			if err != nil {
				return readResourceResult{}, err
			}
			text = content
			break
		}
		return readResourceResult{}, rpcError{Code: errCodeNotFound, Message: "Resource not found"}
	}

	return readResourceResult{
		Contents: []textResourceContents{{
			URI:      uri,
			MimeType: "text/markdown",
			Text:     text,
		}},
	}, nil
}

func (s *Server) callTool(ctx context.Context, params toolCallParams) (toolCallResult, error) {
	switch params.Name {
	case "workspace_status":
		result, err := s.service.WorkspaceStatus(ctx)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderWorkspaceStatus(result)}},
			StructuredContent: result,
		}, nil
	case "search_code":
		var args SearchCodeParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		if strings.TrimSpace(args.Query) == "" {
			return toolCallResult{}, invalidParamsError("search_code requires a non-empty query")
		}
		result, err := s.service.SearchCode(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderSearchResults(result, args.Details)}},
			StructuredContent: result,
		}, nil
	case "index_repository":
		var args IndexRepositoryParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		result, err := s.service.IndexRepository(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderIndexResult(result)}},
			StructuredContent: result,
		}, nil
	default:
		return toolErrorResult(fmt.Errorf("unknown tool %q", params.Name)), nil
	}
}

func decodeToolArgs(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return invalidParamsError("invalid tool arguments")
	}
	return nil
}

func toolErrorResult(err error) toolCallResult {
	return toolCallResult{
		Content: []toolContent{{
			Type: "text",
			Text: err.Error(),
		}},
		IsError: true,
	}
}

func invalidParamsError(message string) error {
	return rpcError{Code: errCodeInvalidParams, Message: message}
}

func negotiatedProtocol(requested string) string {
	requested = strings.TrimSpace(requested)
	if _, ok := supportedProtocols[requested]; ok {
		return requested
	}
	return defaultProtocol
}

func (s *Server) resultEnvelope(id json.RawMessage, result any) responseEnvelope {
	return responseEnvelope{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	}
}

func (s *Server) errorEnvelope(id json.RawMessage, code int, message string) responseEnvelope {
	return responseEnvelope{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: message,
		},
	}
}

func (s *Server) writeError(id json.RawMessage, code int, message string) error {
	return s.writeEnvelope(s.errorEnvelope(id, code, message))
}

func (s *Server) writeEnvelope(envelope responseEnvelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return s.writePayload(payload)
}

func (s *Server) writeBatchResponses(envelopes []responseEnvelope) error {
	payload, err := json.Marshal(envelopes)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writePayloadLocked(payload)
}

func (s *Server) readMessage(r *bufio.Reader) ([]byte, error) {
	switch s.format {
	case transportFormatContentLength:
		return readContentLengthMessage(r, "")
	case transportFormatNewline:
		return readNewlineMessage(r)
	default:
		payload, format, err := detectAndReadMessage(r)
		if err == nil {
			s.format = format
		}
		return payload, err
	}
}

func (s *Server) writePayload(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writePayloadLocked(payload)
}

func (s *Server) writePayloadLocked(payload []byte) error {
	switch s.format {
	case transportFormatContentLength:
		return writeContentLengthMessage(s.writer, payload)
	case transportFormatUnknown, transportFormatNewline:
		return writeNewlineMessage(s.writer, payload)
	default:
		return writeNewlineMessage(s.writer, payload)
	}
}

func detectAndReadMessage(r *bufio.Reader) ([]byte, transportFormat, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				trimmed := bytes.TrimRight(line, "\r\n")
				if len(trimmed) == 0 {
					return nil, transportFormatUnknown, io.EOF
				}
				return trimmed, transportFormatNewline, nil
			}
			return nil, transportFormatUnknown, err
		}

		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			continue
		}
		if isContentLengthHeader(trimmed) {
			payload, err := readContentLengthMessage(r, string(trimmed))
			return payload, transportFormatContentLength, err
		}
		return trimmed, transportFormatNewline, nil
	}
}

func readNewlineMessage(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				trimmed := bytes.TrimRight(line, "\r\n")
				if len(trimmed) == 0 {
					return nil, io.EOF
				}
				return trimmed, nil
			}
			return nil, err
		}

		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 {
			continue
		}
		return trimmed, nil
	}
}

func readContentLengthMessage(r *bufio.Reader, firstLine string) ([]byte, error) {
	contentLength := -1
	if firstLine != "" {
		if n, ok, err := parseContentLengthHeader(firstLine); err != nil {
			return nil, err
		} else if ok {
			contentLength = n
		}
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if n, ok, err := parseContentLengthHeader(trimmed); err != nil {
			return nil, err
		} else if ok {
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseContentLengthHeader(line string) (int, bool, error) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return 0, false, fmt.Errorf("invalid MCP header line %q", line)
	}
	if !strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
		return 0, false, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("invalid Content-Length %q", value)
	}
	return n, true, nil
}

func isContentLengthHeader(line []byte) bool {
	return bytes.HasPrefix(bytes.ToLower(bytes.TrimSpace(line)), []byte("content-length:"))
}

func writeContentLengthMessage(w io.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeNewlineMessage(w io.Writer, payload []byte) error {
	if _, err := w.Write(payload); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}

const (
	docsOverviewResourceURI      = "unch://docs/overview"
	workspaceOverviewResourceURI = "unch://workspace/overview"
	toolResourceTemplateURI      = "unch://tool/{name}"
	toolResourcePrefix           = "unch://tool/"
)

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "workspace_status",
			Description: "Describe the repository, .semsearch state directory, index manifest, and currently configured model/runtime settings.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "search_code",
			Description: "Search the configured repository index for relevant code symbols using semantic, lexical, or mixed retrieval.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural-language or lexical search query.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     50,
						"default":     10,
						"description": "Maximum number of matches to return.",
					},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"auto", "semantic", "lexical"},
						"default":     "auto",
						"description": "Retrieval mode.",
					},
					"max_distance": map[string]any{
						"type":        "number",
						"default":     0.85,
						"description": "Maximum semantic distance in auto and semantic modes; values <= 0 disable filtering.",
					},
					"details": map[string]any{
						"type":        "boolean",
						"default":     false,
						"description": "Include compact symbol metadata in the textual result.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "index_repository",
			Description: "Build or refresh the configured repository index using the server's root, state directory, model, and yzma runtime settings.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"excludes": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Extra exclude globs passed to the indexer.",
					},
					"gitignore": map[string]any{
						"type":        "string",
						"description": "Optional path to a custom .gitignore file.",
					},
					"comment_prefix": map[string]any{
						"type":        "string",
						"description": "Legacy fallback comment prefix for unsupported files.",
						"default":     "@search:",
					},
					"context_prefix": map[string]any{
						"type":        "string",
						"description": "Legacy fallback file-context prefix for unsupported files.",
						"default":     "@filectx:",
					},
				},
			},
		},
	}
}

func resourceDefinitions() []resourceDefinition {
	return []resourceDefinition{
		{
			URI:         docsOverviewResourceURI,
			Name:        "overview",
			Title:       "unch Overview",
			Description: "Quick start and server capabilities for using unch through MCP.",
			MimeType:    "text/markdown",
		},
		{
			URI:         workspaceOverviewResourceURI,
			Name:        "workspace",
			Title:       "unch Workspace Context",
			Description: "Current repository, index, and model configuration snapshot for this unch server.",
			MimeType:    "text/markdown",
		},
	}
}

func resourceTemplateDefinitions() []resourceTemplateDefinition {
	return []resourceTemplateDefinition{
		{
			URITemplate: toolResourceTemplateURI,
			Name:        "tool-docs",
			Title:       "unch Tool Reference",
			Description: "Short markdown reference for a specific unch MCP tool.",
			MimeType:    "text/markdown",
		},
	}
}

func renderDocsOverview() string {
	return strings.Join([]string{
		"# unch MCP",
		"",
		"`unch` exposes semantic code search as MCP tools and resources.",
		"",
		"## Available tools",
		"- `workspace_status`: inspect the configured repository, index files, and runtime/model settings.",
		"- `search_code`: search the indexed repository with a natural-language or lexical query.",
		"- `index_repository`: build or refresh the local semantic index for the current workspace.",
		"",
		"## Quick start",
		"1. Read `unch://workspace/overview` for the current repo and index context.",
		"2. Call `workspace_status` when you need the same data as structured JSON.",
		"3. Call `search_code` with a short query like `mcp server`, `workspace status`, or `cli start`.",
		"4. Open `unch://tool/{name}` for argument-level docs, for example `unch://tool/search_code`.",
		"",
		"## Notes",
		"- `search_code` requires a non-empty `query`.",
		"- `index_repository` updates the local `.semsearch` state for this workspace.",
	}, "\n")
}

func renderWorkspaceOverviewResource(result WorkspaceStatusResult) string {
	lines := []string{
		"# unch Workspace Context",
		"",
		"## Current workspace",
		fmt.Sprintf("- root: `%s`", result.Root),
		fmt.Sprintf("- state_dir: `%s`", result.StateDir),
		fmt.Sprintf("- index_db: `%s`", result.IndexDB),
	}
	if result.ManifestSource != "" || result.ManifestVersion > 0 {
		lines = append(lines, fmt.Sprintf("- manifest: `%s v%d`", fallbackString(result.ManifestSource, "local"), result.ManifestVersion))
	}
	if result.RemoteCIURL != "" {
		lines = append(lines, fmt.Sprintf("- remote_ci: `%s`", result.RemoteCIURL))
	}
	if result.RequestedModel != "" {
		lines = append(lines, fmt.Sprintf("- requested_model: `%s`", result.RequestedModel))
	}
	if result.ModelID != "" {
		lines = append(lines, fmt.Sprintf("- model_id: `%s`", result.ModelID))
	}
	if result.ResolvedModel != "" {
		lines = append(lines, fmt.Sprintf("- resolved_model: `%s`", result.ResolvedModel))
	}
	if result.RequestedLib != "" {
		lines = append(lines, fmt.Sprintf("- requested_lib: `%s`", result.RequestedLib))
	}
	if result.ResolvedLib != "" {
		lines = append(lines, fmt.Sprintf("- resolved_lib: `%s`", result.ResolvedLib))
	}
	if result.ContextSize > 0 {
		lines = append(lines, fmt.Sprintf("- ctx_size: `%d`", result.ContextSize))
	}
	if result.IndexPresent {
		lines = append(lines, "- index_present: `yes`")
	} else {
		lines = append(lines, "- index_present: `no`")
	}
	lines = append(lines,
		"",
		"## Available docs",
		"- `unch://docs/overview`",
		"- `unch://tool/workspace_status`",
		"- `unch://tool/search_code`",
		"- `unch://tool/index_repository`",
	)
	return strings.Join(lines, "\n")
}

func renderToolResource(name string) (string, error) {
	switch name {
	case "workspace_status":
		return strings.Join([]string{
			"# `workspace_status`",
			"",
			"Returns a structured snapshot of the current repository, `.semsearch` state, manifest, and model/runtime configuration.",
			"",
			"## Arguments",
			"- none",
			"",
			"## Returns",
			"- `root`, `state_dir`, `index_db`",
			"- manifest fields such as `manifest_source` and `manifest_version` when available",
			"- requested/resolved model or runtime fields when configured",
			"- `index_present` to show whether a local index already exists",
		}, "\n"), nil
	case "search_code":
		return strings.Join([]string{
			"# `search_code`",
			"",
			"Search the indexed repository for relevant code symbols using semantic, lexical, or mixed retrieval.",
			"",
			"## Arguments",
			"- `query` (required): natural-language or lexical search query",
			"- `limit`: maximum number of matches, default `10`",
			"- `mode`: one of `auto`, `semantic`, `lexical`",
			"- `max_distance`: semantic distance threshold; values `<= 0` disable filtering",
			"- `details`: include compact symbol metadata in the textual response",
			"",
			"## Example",
			"- query: `mcp server`",
			"- query: `workspace status`, mode: `lexical`",
		}, "\n"), nil
	case "index_repository":
		return strings.Join([]string{
			"# `index_repository`",
			"",
			"Build or refresh the local `.semsearch` index for the current workspace using the configured model and runtime.",
			"",
			"## Arguments",
			"- `excludes`: extra exclude globs passed to the indexer",
			"- `gitignore`: optional path to a custom `.gitignore` file",
			"- `comment_prefix`: legacy fallback comment prefix for unsupported files",
			"- `context_prefix`: legacy fallback file-context prefix for unsupported files",
			"",
			"## Returns",
			"- indexed file/symbol counts",
			"- manifest version",
			"- state directory and active model ID",
		}, "\n"), nil
	default:
		return "", rpcError{Code: errCodeNotFound, Message: "Resource not found"}
	}
}

func renderWorkspaceStatus(result WorkspaceStatusResult) string {
	lines := []string{
		"unch MCP workspace",
		fmt.Sprintf("root: %s", result.Root),
		fmt.Sprintf("state_dir: %s", result.StateDir),
		fmt.Sprintf("index_db: %s", result.IndexDB),
	}
	if result.ManifestSource != "" || result.ManifestVersion > 0 {
		lines = append(lines, fmt.Sprintf("manifest: %s v%d", fallbackString(result.ManifestSource, "local"), result.ManifestVersion))
	}
	if result.RemoteCIURL != "" {
		lines = append(lines, fmt.Sprintf("remote_ci: %s", result.RemoteCIURL))
	}
	if result.RequestedModel != "" {
		lines = append(lines, fmt.Sprintf("requested_model: %s", result.RequestedModel))
	}
	if result.ModelID != "" {
		lines = append(lines, fmt.Sprintf("model_id: %s", result.ModelID))
	}
	if result.ResolvedModel != "" {
		lines = append(lines, fmt.Sprintf("resolved_model: %s", result.ResolvedModel))
	}
	if result.RequestedLib != "" {
		lines = append(lines, fmt.Sprintf("requested_lib: %s", result.RequestedLib))
	}
	if result.ResolvedLib != "" {
		lines = append(lines, fmt.Sprintf("resolved_lib: %s", result.ResolvedLib))
	}
	if result.ContextSize > 0 {
		lines = append(lines, fmt.Sprintf("ctx_size: %d", result.ContextSize))
	}
	if result.IndexPresent {
		lines = append(lines, "index_present: yes")
	} else {
		lines = append(lines, "index_present: no")
	}
	return strings.Join(lines, "\n")
}

func renderSearchResults(result SearchCodeResult, details bool) string {
	if len(result.Hits) == 0 {
		return fmt.Sprintf("No matches found for %q.", result.Query)
	}

	lines := []string{
		fmt.Sprintf("Found %d matches for %q (%s).", result.ResultCount, result.Query, result.Mode),
	}
	for i, hit := range result.Hits {
		lines = append(lines, fmt.Sprintf("%2d. %s:%d  %s", i+1, hit.Path, hit.Line, hit.Metric))
		if details {
			if value := compactField(hit.Kind, 80); value != "" {
				lines = append(lines, "    kind: "+value)
			}
			if value := compactField(hit.Name, 120); value != "" {
				lines = append(lines, "    name: "+value)
			}
			if value := compactField(hit.QualifiedName, 160); value != "" {
				lines = append(lines, "    qualified: "+value)
			}
			if value := compactField(hit.Signature, 200); value != "" {
				lines = append(lines, "    signature: "+value)
			}
			if value := compactField(hit.Documentation, 220); value != "" {
				lines = append(lines, "    docs: "+value)
			}
			if value := compactField(hit.Body, 220); value != "" {
				lines = append(lines, "    body: "+value)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func renderIndexResult(result IndexRepositoryResult) string {
	lines := []string{
		fmt.Sprintf("Indexed %d symbols in %d files.", result.IndexedSymbols, result.IndexedFiles),
		fmt.Sprintf("manifest version: %d", result.Version),
		fmt.Sprintf("state_dir: %s", result.StateDir),
		fmt.Sprintf("model_id: %s", result.ModelID),
	}
	if result.DetachedRemoteBinding {
		lines = append(lines, "note: local indexing detached the previous remote CI binding and switched the manifest back to local mode")
	}
	return strings.Join(lines, "\n")
}

func compactField(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type resourceReadParams struct {
	URI string `json:"uri"`
}

type toolCallResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type listToolsResult struct {
	Tools      []toolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type resourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type listResourcesResult struct {
	Resources  []resourceDefinition `json:"resources"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

type resourceTemplateDefinition struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type listResourceTemplatesResult struct {
	ResourceTemplates []resourceTemplateDefinition `json:"resourceTemplates"`
	NextCursor        string                       `json:"nextCursor,omitempty"`
}

type textResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type readResourceResult struct {
	Contents []textResourceContents `json:"contents"`
}

type rpcError struct {
	Code    int
	Message string
}

func (e rpcError) Error() string {
	return e.Message
}
