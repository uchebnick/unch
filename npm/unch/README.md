# @uchebnick/unch

npm installer wrapper for the [`unch`](https://github.com/uchebnick/unch) semantic code search CLI.

```bash
npm install -g @uchebnick/unch
unch --version
unch --help
```

The package downloads the matching native binary from GitHub Releases during `postinstall`.

## MCP

For MCP clients, use:

- Name: `unch`
- Command: `unch-mcp`
- Arguments: leave empty
- Working directory: the repository you want to search

`unch-mcp` is a small launcher for `unch start mcp`. The MCP server also exposes a prompt named `unch`, so clients that render MCP prompts as slash commands can show it as `/unch`.

If your client supports MCP prompts, run `/unch` before a codebase question to nudge the assistant to call `workspace_status`, `search_code`, and `index_repository` in the right order.

Supported targets:

- macOS `arm64`, `x64`
- Linux `arm64`, `x64`
- Windows `arm64`, `x64`

Environment overrides:

- `UNCH_NPM_TAG=v0.3.12` downloads a specific release tag.
- `UNCH_BINARY_URL=https://...` or `UNCH_BINARY_URL=file:///...` downloads or reads a custom archive.
- `UNCH_SKIP_DOWNLOAD=1` skips download for package smoke tests.
