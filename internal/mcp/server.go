package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type Server struct {
	service Service
	writer  io.Writer
	framing messageFraming
	writeMu sync.Mutex
}

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

		payload, framing, err := readMCPMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		s.framing = framing
		if err := s.handlePayload(ctx, payload); err != nil {
			return err
		}
	}
}

func (s *Server) handlePayload(ctx context.Context, payload []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(payload), []byte("[")) {
		return s.writeError(nil, errCodeInvalidReq, "batch requests are not supported")
	}

	var req requestEnvelope
	if err := json.Unmarshal(payload, &req); err != nil {
		return s.writeError(nil, errCodeParse, "failed to decode JSON request")
	}

	if strings.TrimSpace(req.JSONRPC) != jsonRPCVersion || strings.TrimSpace(req.Method) == "" {
		if req.ID != nil {
			return s.writeError(req.ID, errCodeInvalidReq, "invalid JSON-RPC request")
		}
		return nil
	}

	if req.ID == nil {
		return nil
	}
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
				"prompts": map[string]any{
					"listChanged": false,
				},
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			ServerInfo: serverInfo{
				Name:    "unch",
				Version: s.service.Version(),
			},
			Instructions: serverInstructions(),
		}
		return s.resultEnvelope(req.ID, result)
	case "ping":
		return s.resultEnvelope(req.ID, map[string]any{})
	case "tools/list":
		return s.resultEnvelope(req.ID, listToolsResult{Tools: toolDefinitions()})
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
	case "prompts/list":
		return s.resultEnvelope(req.ID, listPromptsResult{Prompts: promptDefinitions()})
	case "prompts/get":
		var params promptGetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorEnvelope(req.ID, errCodeInvalidParams, "invalid prompts/get params")
		}
		result, err := getPrompt(params)
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

func (s *Server) resultEnvelope(id json.RawMessage, result any) responseEnvelope {
	return responseEnvelope{
		JSONRPC: jsonRPCVersion,
		ID:      responseID(id),
		Result:  result,
	}
}

func (s *Server) errorEnvelope(id json.RawMessage, code int, message string) responseEnvelope {
	return responseEnvelope{
		JSONRPC: jsonRPCVersion,
		ID:      responseID(id),
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

func (s *Server) writePayload(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeMCPMessage(s.writer, payload, s.framing)
}

func serverInstructions() string {
	return strings.Join([]string{
		"unch provides local-first semantic code search for the current repository workspace.",
		"When the client knows the repository path, pass directory as an absolute path on workspace_status, search_code, and index_repository.",
		"Recommended workflow for agents:",
		"1. Call workspace_status first to learn the root, state directory, selected provider/model, and whether an index exists.",
		"2. Before reading many files or using broad grep-style exploration, call search_code with a concise natural-language or identifier query.",
		"3. If search_code says there is no active snapshot for the configured provider/model, call index_repository once, then retry search_code.",
		"4. Do not call index_repository repeatedly unless files changed, the index is missing, or the user explicitly asks to rebuild.",
		"5. Use mode=\"auto\" by default, mode=\"lexical\" for exact identifiers or strings, and mode=\"semantic\" for meaning-based discovery.",
		"6. Set details=true when you need signatures, symbol kind/name, docs, or compact body snippets for deciding which files to open.",
		"7. Treat results as ranked candidates, not a complete proof. Open the returned paths when you need exact implementation details.",
		"Provider/model snapshots are isolated. The MCP process searches only the provider/model selected when it was launched.",
	}, "\n")
}
