package mcp

import (
	"fmt"
	"strings"
)

type promptDefinition struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []promptArgument `json:"arguments,omitempty"`
}

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type listPromptsResult struct {
	Prompts    []promptDefinition `json:"prompts"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type promptGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

type promptGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []promptMessage `json:"messages"`
}

type promptMessage struct {
	Role    string        `json:"role"`
	Content promptContent `json:"content"`
}

type promptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func promptDefinitions() []promptDefinition {
	return []promptDefinition{
		{
			Name:        "unch",
			Title:       "Search this repository with unch",
			Description: "Use unch semantic code search before broad file reads or grep-style exploration.",
			Arguments: []promptArgument{{
				Name:        "query",
				Description: "Optional task, feature, bug, identifier, or concept to search for.",
			}},
		},
	}
}

func getPrompt(params promptGetParams) (promptGetResult, error) {
	name := strings.TrimSpace(params.Name)
	if name != "unch" {
		return promptGetResult{}, invalidParamsError(fmt.Sprintf("unknown prompt %q", params.Name))
	}

	query := strings.TrimSpace(params.Arguments["query"])
	text := strings.Join([]string{
		"Use the unch MCP tools to understand this repository before opening many files.",
		"First call workspace_status.",
		"If there is no active index snapshot, call index_repository once and retry.",
		"Then call search_code with concise semantic or identifier queries.",
		"Use details=true when signatures, docs, or body snippets help choose exact files.",
		"Treat search results as ranked candidates and open returned paths for exact implementation details.",
	}, "\n")
	if query != "" {
		text += "\n\nInitial search/task query: " + query
	}

	return promptGetResult{
		Description: "Guide the assistant to use unch semantic code search for this workspace.",
		Messages: []promptMessage{{
			Role: "user",
			Content: promptContent{
				Type: "text",
				Text: text,
			},
		}},
	}, nil
}
