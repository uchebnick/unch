package mcp

import "context"

type Service interface {
	Version() string
	WorkspaceStatus(context.Context, WorkspaceStatusParams) (WorkspaceStatusResult, error)
	SearchCode(context.Context, SearchCodeParams) (SearchCodeResult, error)
	IndexRepository(context.Context, IndexRepositoryParams) (IndexRepositoryResult, error)
}

type WorkspaceStatusParams struct {
	Directory string `json:"directory,omitempty"`
}

type SearchCodeParams struct {
	Directory   string   `json:"directory,omitempty"`
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
	Directory     string   `json:"directory,omitempty"`
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
