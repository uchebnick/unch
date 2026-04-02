<p align="center">
  <img src="docs/assets/unch-logo.svg" alt="unch logo" width="300">
</p>

# unch

[![Release](https://img.shields.io/github/v/release/uchebnick/unch?display_name=tag)](https://github.com/uchebnick/unch/releases/latest)
[![Homebrew Tap](https://img.shields.io/badge/Homebrew-uchebnick%2Ftap-FBB040?logo=homebrew&logoColor=white)](https://github.com/uchebnick/homebrew-tap)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/uchebnick/unch/gh-pages/coverage/coverage-badge.json)](https://github.com/uchebnick/unch/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://github.com/uchebnick/unch)

**Local-first semantic code search for real code objects.**

`unch` indexes functions, methods, types, classes, interfaces, and attached docs with Tree-sitter, then lets you search them from the terminal. It is built for the moment when you know what the code does, but do not remember the symbol name, file path, or exact text. By default everything stays local to the repository; GitHub Actions publishing is optional.

<p align="center">
  <img src="docs/assets/unch-demo.svg" alt="Terminal demo of unch indexing gorilla/mux and returning semantic search matches" width="920">
</p>

## Why `unch`

- **Local-first by default.** The index, model cache, and search flow stay on your machine unless you explicitly publish a remote index.
- **Symbol-aware search.** `unch` indexes top-level API and attached docs for `Go`, `TypeScript`, `JavaScript`, and `Python` instead of relying on manual inline prefixes.
- **Terminal-native workflow.** Index once, search from the CLI, and optionally keep a repo-wide index fresh through GitHub Actions.

## Install

Homebrew is still the most polished install path on macOS:

```bash
brew install uchebnick/tap/unch
```

For release-based installs without Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/uchebnick/unch/main/install.sh | sh
```

On Windows, use the PowerShell installer:

```powershell
iwr https://raw.githubusercontent.com/uchebnick/unch/main/install/install.ps1 -useb | iex
```

For source-based installation, use the canonical module path:

```bash
go install github.com/uchebnick/unch@latest
```

If you want to hack on the project directly, build it from the current checkout:

```bash
go build -o unch .
```

Published release archives currently cover:

- macOS: `arm64`, `x86_64`
- Linux: `arm64`, `x86_64`
- Windows: `arm64`, `x86_64` (`unch.exe`)

On those supported Linux and Windows targets, the installers use the published release archives by default, so Go is not required. `install.sh` and `install/install.ps1` only fall back to `go install` when a matching release archive is not available.

See [Compatibility](docs/compatibility.md) for the support matrix and upgrade rules, and [Benchmarks](docs/benchmarks.md) for the pinned benchmark suite, score formula, and current sample results.

Model selection accepts either a known model id or a direct `.gguf` path:

```bash
unch index --model embeddinggemma
unch index --model qwen3
unch search --model qwen3 "create a new router"
```

## 30-Second Path

```bash
cd path/to/repo
unch index --root .
unch search "create a new router"
unch search --details "get path variables from a request"
```

Real output from indexing [`gorilla/mux`](https://github.com/gorilla/mux):

```text
$ unch index --root .
Loaded model       dim=768
Indexed 278 symbols in 16 files

$ unch search "create a new router"
1. mux.go:32  0.7747
2. mux.go:314 0.8135

$ unch search --details "get path variables from a request"
1. mux.go:466  0.7991
   kind: function
   name: Vars
   signature: func Vars(r *http.Request) map[string]string
   docs: Vars returns the route variables for the current request, if any.
```

First run may download the default embedding model, download local `yzma` runtime libraries, and create `./.semsearch/`.

Each model keeps its own active index snapshot. Rebuilding `qwen3` does not replace the active `embeddinggemma` snapshot until the new run finishes successfully.

## What It Supports Today

- Tree-sitter indexing for `Go`, `TypeScript`, `JavaScript`, and `Python`
- Top-level API objects and attached documentation
- Local indexing and search first, optional remote publishing through GitHub Actions
- `auto`, `semantic`, and `lexical` search modes
- Legacy `--comment-prefix` and `--context-prefix` only as fallback for unsupported files or parser failures

## When `unch` Helps

- You know the behavior you want, but not the exact symbol name.
- You want documentation and API surface searchable together.
- You want semantic search over a repo without sending code to a hosted service.

## Use Something Else When

- `grep` or `rg` is better for exact strings, literals, or known filenames.
- LSP or IDE navigation is better when you already know the symbol family and want jump-to-definition or refactors.
- Hosted code search is better when you need cross-repository search across an entire organization.

## Core Commands

### `index`

Build or refresh the local index for a repository.

```bash
unch index --root .
```

Useful flags:

- `--exclude` to skip generated, vendor, or irrelevant paths
- `--model` to use `embeddinggemma`, `qwen3`, or a custom `.gguf` path
- `--lib` to use an existing `yzma` runtime directory

### `search`

Query the current index.

```bash
unch search "sqlite schema"
unch search --mode lexical "Run"
```

Useful flags:

- `--mode` for `auto`, `semantic`, or `lexical`
- `--limit` to control result count
- `--max-distance` to narrow semantic matches
- `--model` to search with `embeddinggemma`, `qwen3`, or a custom `.gguf` path
- `--details` to print symbol metadata, signature, docs, and body context for each match

## Remote / CI

Remote indexing is optional and stays secondary to the local workflow. Use it when you want GitHub Actions to publish an index for the repository.

Create the workflow scaffold:

```bash
unch create ci
```

Bind the local repository state to a GitHub repo or workflow URL:

```bash
unch bind ci https://github.com/uchebnick/unch
```

After the workflow is committed and runs successfully once, `unch search` can refresh the published index automatically when a newer remote version exists. Use `unch remote sync` when you want to force a refresh before searching.

The benchmark suite runs in CI only on release tags, using the smaller smoke profile so ordinary pushes stay fast while coverage still runs on every push.

## Contributing and Feedback

- See [CONTRIBUTING.md](CONTRIBUTING.md) for the local dev loop.
- Open a [bug report](https://github.com/uchebnick/unch/issues/new?template=bug_report.md) if search quality or indexing behavior looks wrong.
- Open a [feature request](https://github.com/uchebnick/unch/issues/new?template=feature_request.md) if you want new language support, ranking behavior, or workflow features.
