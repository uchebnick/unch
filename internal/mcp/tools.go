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

func directoryProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Optional absolute repository/workspace path to operate on. Pass the active workspace path when the MCP server may have been launched from another directory.",
	}
}

func githubTargetProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "GitHub repository URL such as https://github.com/owner/repo, or a full workflow URL such as https://github.com/owner/repo/actions/workflows/unch-index.yml.",
	}
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "workspace_status",
			Description: "Call this first. Returns the current repository root, .semsearch state directory, selected provider/model, manifest data, and whether a local index is already present.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
				},
			},
		},
		{
			Name:        "search_code",
			Description: "Search indexed code symbols before opening many files. Use concise natural-language queries for concepts, exact names with lexical mode, and details=true when signatures/docs/body snippets would help choose files to inspect.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
					"query": map[string]any{
						"type":        "string",
						"description": "Short natural-language, identifier, API, behavior, or error-handling query. Examples: \"request router middleware\", \"UserRepository Create\", \"MCP Content-Length framing\".",
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     50,
						"default":     10,
						"description": "Maximum number of ranked candidate symbols to return. Use a small limit first; increase it only when the first results are not enough.",
					},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"auto", "semantic", "lexical"},
						"default":     "auto",
						"description": "Retrieval mode. Use auto by default, semantic for meaning-based discovery, and lexical for exact identifiers or strings.",
					},
					"max_distance": map[string]any{
						"type":        "number",
						"default":     0.85,
						"description": "Maximum semantic distance in auto and semantic modes. Lower is stricter; values <= 0 disable distance filtering.",
					},
					"details": map[string]any{
						"type":        "boolean",
						"default":     false,
						"description": "When true, include symbol kind/name, qualified name, signature, docs, and compact body snippets in addition to paths and lines.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "index_repository",
			Description: "Build or refresh the local index for this workspace. Call when workspace_status shows no index, search_code reports no active snapshot, files changed and fresh search is needed, or the user asks to rebuild. Avoid repeated rebuilds during normal exploration.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
					"excludes": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Extra exclude globs for generated, dependency, build, cache, or otherwise noisy paths, for example node_modules, dist, build, vendor, .next.",
					},
					"gitignore": map[string]any{
						"type":        "string",
						"description": "Optional path to a custom .gitignore file. Leave empty to use the repository default behavior.",
					},
					"comment_prefix": map[string]any{
						"type":        "string",
						"description": "Legacy fallback comment prefix for unsupported files or parser failures. Most agents should leave this at the default.",
						"default":     "@search:",
					},
					"context_prefix": map[string]any{
						"type":        "string",
						"description": "Legacy fallback file-context prefix for unsupported files or parser failures. Most agents should leave this at the default.",
						"default":     "@filectx:",
					},
				},
			},
		},
		{
			Name:        "create_ci_workflow",
			Description: "Create the default GitHub Actions workflow that builds and publishes a remote unch index. Use this when the user asks to set up CI-backed indexing.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
				},
			},
		},
		{
			Name:        "bind_remote_ci",
			Description: "Bind this workspace manifest to a GitHub repository or unch remote-index workflow, enabling later remote_sync_index calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
					"target":    githubTargetProperty(),
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "remote_sync_index",
			Description: "Refresh the local index from a previously bound remote GitHub Actions workflow. Call this before local reindexing when workspace_status shows a remote_ci binding.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
					"allow_missing": map[string]any{
						"type":        "boolean",
						"default":     false,
						"description": "When true, missing or incompatible remote indexes return a non-error result with a note so bootstrap flows can continue.",
					},
				},
			},
		},
		{
			Name:        "remote_download_index",
			Description: "Download and activate a published unch index artifact for a specific commit without binding the workspace to ongoing remote sync.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": directoryProperty(),
					"target":    githubTargetProperty(),
					"commit": map[string]any{
						"type":        "string",
						"description": "Commit SHA whose search index artifact should be downloaded.",
					},
				},
				"required": []string{"target", "commit"},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, params toolCallParams) (toolCallResult, error) {
	switch params.Name {
	case "workspace_status":
		var args WorkspaceStatusParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		result, err := s.service.WorkspaceStatus(ctx, args)
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
	case "create_ci_workflow":
		var args CreateCIWorkflowParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		result, err := s.service.CreateCIWorkflow(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderCreateCIWorkflowResult(result)}},
			StructuredContent: result,
		}, nil
	case "bind_remote_ci":
		var args BindRemoteCIParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		if strings.TrimSpace(args.Target) == "" {
			return toolCallResult{}, invalidParamsError("bind_remote_ci requires a non-empty target")
		}
		result, err := s.service.BindRemoteCI(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderBindRemoteCIResult(result)}},
			StructuredContent: result,
		}, nil
	case "remote_sync_index":
		var args RemoteSyncIndexParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		result, err := s.service.RemoteSyncIndex(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderRemoteSyncIndexResult(result)}},
			StructuredContent: result,
		}, nil
	case "remote_download_index":
		var args RemoteDownloadIndexParams
		if err := decodeToolArgs(params.Arguments, &args); err != nil {
			return toolCallResult{}, err
		}
		if strings.TrimSpace(args.Target) == "" {
			return toolCallResult{}, invalidParamsError("remote_download_index requires a non-empty target")
		}
		if strings.TrimSpace(args.Commit) == "" {
			return toolCallResult{}, invalidParamsError("remote_download_index requires a non-empty commit")
		}
		result, err := s.service.RemoteDownloadIndex(ctx, args)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: renderRemoteDownloadIndexResult(result)}},
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
