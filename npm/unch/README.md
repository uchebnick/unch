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

## MCP for Codex

Codex setup is explicit:

```bash
unch codex install
```

Then restart Codex. Codex will start the MCP server through the registered `unch-mcp` command and use the installed `unch` skill when semantic code search helps.

`unch-mcp` is a small launcher for `unch start mcp`; you normally do not need to run it by hand.

The MCP server exposes local search tools plus remote-index helpers for GitHub Actions-backed indexes: `create_ci_workflow`, `bind_remote_ci`, `remote_sync_index`, and `remote_download_index`.

Supported targets:

- macOS `arm64`, `x64`
- Linux `arm64`, `x64`
- Windows `arm64`, `x64`

Environment overrides:

- `UNCH_NPM_TAG=v0.4.1` downloads a specific release tag.
- `UNCH_BINARY_URL=https://...` or `UNCH_BINARY_URL=file:///...` downloads or reads a custom archive.
- `UNCH_SKIP_DOWNLOAD=1` skips download for package smoke tests.
