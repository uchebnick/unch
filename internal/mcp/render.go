package mcp

import (
	"fmt"
	"strings"
)

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
	if result.RequestedProvider != "" {
		lines = append(lines, fmt.Sprintf("requested_provider: %s", result.RequestedProvider))
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
		if !details {
			continue
		}
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
