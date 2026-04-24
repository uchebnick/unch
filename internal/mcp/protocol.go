package mcp

import (
	"encoding/json"
	"strings"
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
)

var supportedProtocols = map[string]struct{}{
	legacyProtocol:      {},
	toolsProtocol:       {},
	rootsProtocol:       {},
	latestKnownProtocol: {},
}

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
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

type rpcError struct {
	Code    int
	Message string
}

func (e rpcError) Error() string {
	return e.Message
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

func responseID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}
