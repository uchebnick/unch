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

		payload, err := readContentLengthMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
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
				"tools": map[string]any{
					"listChanged": false,
				},
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
	return writeContentLengthMessage(s.writer, payload)
}
