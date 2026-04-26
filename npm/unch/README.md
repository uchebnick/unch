# @uchebnick/unch

npm installer wrapper for the [`unch`](https://github.com/uchebnick/unch) semantic code search CLI.

```bash
npm install -g @uchebnick/unch
unch --version
unch --help
```

The package downloads the matching native binary from GitHub Releases during `postinstall`.

## Codex setup

To make unch available in Codex as both MCP tools and the `unch` skill, run:

```bash
unch codex install
```

Then restart Codex. The installer uses `codex mcp add` for the MCP server and writes the skill to `~/.codex/skills/unch/SKILL.md`.

The npm `postinstall` step does not modify your Codex config automatically. This is intentional: installing a package should not silently mutate `~/.codex/config.toml`.

## MCP

For MCP clients, use:

- Name: `unch`
- Command: `unch-mcp`
- Arguments: leave empty
- Working directory: the repository you want to search

`unch-mcp` is a small launcher for `unch start mcp`.

For Codex CLI specifically, `unch codex install` also creates a local reusable skill, so Codex knows when to call `workspace_status`, `search_code`, and `index_repository` in the right order.

Supported targets:

- macOS `arm64`, `x64`
- Linux `arm64`, `x64`
- Windows `arm64`, `x64`

Environment overrides:

- `UNCH_NPM_TAG=v0.4.1` downloads a specific release tag.
- `UNCH_BINARY_URL=https://...` or `UNCH_BINARY_URL=file:///...` downloads or reads a custom archive.
- `UNCH_SKIP_DOWNLOAD=1` skips download for package smoke tests.
