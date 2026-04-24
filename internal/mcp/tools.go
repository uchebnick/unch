package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
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

func (s *Server) callTool(ctx context.Context, params toolCallParams) (toolCallResult, error) {
	switch params.Name {
	case "workspace_status":
		if err := decodeToolArgs(params.Arguments, &struct{}{}); err != nil {
			return toolCallResult{}, err
		}
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
