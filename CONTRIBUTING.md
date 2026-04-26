# Contributing

Thanks for working on `unch`. This project is a local-first semantic code search CLI with optional remote indexing, MCP integration, and npm/Codex packaging. This guide is meant to make the codebase less mysterious before you start changing it.

## Project Map

```text
.
|-- cmd/
|   |-- unch/                 CLI entrypoint for the main binary
|   `-- bench/                benchmark runner entrypoint
|-- internal/
|   |-- bench/                benchmark suites, scoring, and CLI adapters
|   |-- cli/                  command parsing and command orchestration
|   |-- embed/                embedding provider interface and shared formatting
|   |   |-- llama/            local llama.cpp / yzma GGUF embedder
|   |   `-- openrouter/       OpenRouter embedding provider
|   |-- filehashdb/           file hash snapshots used for incremental indexing
|   |-- indexdb/              SQLite/vector index storage and provider/model snapshots
|   |-- indexing/             repository scanning and indexing service
|   |   `-- treesitter/       language-specific Tree-sitter symbol extraction
|   |-- mcp/                  stdio MCP protocol, tool schemas, and renderers
|   |-- modelcatalog/         built-in model metadata
|   |-- runtime/              model downloads, yzma runtime resolution, progress UI
|   |-- search/               semantic/lexical search service
|   |-- semsearch/            `.semsearch` paths, manifests, tokens, remote state
|   `-- termui/               terminal progress/session helpers
|-- benchmarks/
|   `-- suites/               checked-in benchmark suite definitions
|-- docs/                     GitHub-facing docs and assets
|-- mintlify/                 public docs site source
|-- npm/unch/                 npm wrapper, native binary installer, Codex setup
|-- install/                  PowerShell installer
|-- scripts/                  release, benchmark, docs, and asset helper scripts
`-- .github/
    |-- workflows/            CI, release, remote index, docs sync
    `-- releases/             release note markdown files
```

## Core Flows

### CLI command flow

`cmd/unch/main.go` calls `internal/cli.Run`. Command-specific files in `internal/cli` parse flags, resolve `.semsearch` paths, prepare an embedder, and call the lower-level services.

Use `internal/cli` for command behavior and UX. Keep storage, indexing, search, and provider logic in their own packages.

### Indexing flow

`internal/cli/index.go` prepares paths and an embedder, then calls `internal/indexing.Service`.

The indexing service:

- walks repository files with `internal/indexing.Scanner`
- extracts symbols through `internal/indexing/treesitter`
- embeds each indexed symbol through `internal/embed`
- writes vectors and metadata to `internal/indexdb`
- records file hashes in `internal/filehashdb`
- updates `.semsearch/manifest.json` through `internal/semsearch`

Add language support in `internal/indexing/treesitter`, not in the CLI.

### Search flow

`internal/cli/search.go` opens the active provider/model snapshot from `internal/indexdb`, embeds the query, then calls `internal/search.Service`.

Search modes:

- `auto` combines lexical and semantic behavior
- `semantic` uses embeddings and distance filtering
- `lexical` is for exact names, identifiers, and strings

### Embedding providers

The provider interface lives in `internal/embed`.

Current providers:

- `llama.cpp` via `internal/embed/llama`
- `openrouter` via `internal/embed/openrouter`

Provider/model identity matters. Index snapshots are isolated by provider, model, and vector dimension so different embedding backends can coexist in the same `.semsearch` directory.

### MCP and Codex flow

`internal/mcp` owns the MCP protocol layer: framing, JSON-RPC methods, tool schemas, tool calls, and human-readable renderers.

`internal/cli/start.go` starts the MCP server. `internal/cli/mcp_backend.go` adapts MCP tool calls to the same indexing/search services used by the CLI.

The npm wrapper in `npm/unch` installs the native binary and provides:

- `unch` for normal CLI usage
- `unch-mcp` as a small launcher for `unch start mcp`
- `unch codex install` to register the MCP server and install the Codex skill

For Codex, users should run:

```bash
npm install -g @uchebnick/unch
unch codex install
```

Then they restart Codex. Codex starts the MCP server automatically.

### Remote indexing flow

Remote indexing is optional. `unch create ci` generates `.github/workflows/unch-index.yml`, and `unch bind ci` stores the remote binding in `.semsearch/manifest.json`.

Remote sync logic lives mostly in `internal/semsearch` and `internal/cli/remote.go`.

## Common Change Points

### Add a CLI command

1. Add command parsing/dispatch in `internal/cli/root.go`.
2. Put behavior in a focused `internal/cli/<command>.go` file.
3. Add help text in `internal/cli/help.go`.
4. Add CLI tests in `internal/cli`.
5. Update README and Mintlify docs if the command is user-facing.

### Add a language parser

1. Add language-specific extraction in `internal/indexing/treesitter/<language>.go`.
2. Register the language in `internal/indexing/treesitter/parser.go`.
3. Add fixtures/tests in `internal/indexing/treesitter_test.go`.
4. Update README, Mintlify compatibility docs, and release notes.

### Add an embedding provider

1. Implement `internal/embed.Embedder`.
2. Add provider parsing/identity in `internal/embed/provider.go`.
3. Wire provider construction through `internal/cli/embedding.go`.
4. Ensure index snapshots are provider/model-specific.
5. Add token/config docs if the provider needs credentials.

### Change MCP behavior

1. Update tool schemas in `internal/mcp/tools.go`.
2. Update MCP params/results in `internal/mcp/types.go`.
3. Update backend behavior in `internal/cli/mcp_backend.go`.
4. Add protocol/tool tests in `internal/mcp`.
5. Update `npm/unch/scripts/codex-install.js` if the Codex skill text changes.
6. Update README, npm README, and Mintlify MCP docs.

## Local Setup

Build and test the current checkout:

```bash
go test ./...
go build -o unch ./cmd/unch
```

Run an end-to-end local smoke test:

```bash
go run ./cmd/unch index --root .
go run ./cmd/unch search --root . "command dispatch"
```

First local `llama.cpp` usage may download the default embedding model, fetch managed `yzma` runtime libraries, and create `./.semsearch/`.

## npm Wrapper Checks

When touching `npm/unch`, run:

```bash
cd npm/unch
npm test
```

If MCP launcher behavior changes, build a local binary first and run the smoke test:

```bash
go build -o npm/unch/vendor/unch ./cmd/unch
cd npm/unch
npm run test:mcp
```

Remove `npm/unch/vendor/` before committing.

## Documentation

Keep these in sync for user-facing changes:

- `README.md`
- `docs/`
- `mintlify/`
- `npm/unch/README.md`
- `.github/releases/<tag>.md` for release notes

For install, MCP, provider/model, or compatibility changes, update both GitHub docs and Mintlify docs.

## Before Opening a PR

Run the narrowest relevant tests plus formatting checks:

```bash
go test ./...
git diff --check
```

For docs-only changes, `git diff --check` is usually enough.

Before opening a PR, check:

- the branch contains only the intended files
- generated or downloaded files are not staged
- docs match the actual CLI behavior
- release notes mention upgrade-impacting behavior

## Reporting Issues

- Bugs: include OS, architecture, `unch --version`, provider/model, repository language, command, and unexpected result.
- Search quality reports: include the query, actual results, and the result you expected to rank higher.
- MCP/Codex issues: include whether `unch codex install` was run, whether Codex was restarted, and any MCP error text.
- CI or remote issues: include the workflow URL or failing run URL when possible.
