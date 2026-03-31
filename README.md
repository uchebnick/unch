# unch

[![Release](https://img.shields.io/github/v/release/uchebnick/unch?display_name=tag)](https://github.com/uchebnick/unch/releases/latest)
[![Homebrew Tap](https://img.shields.io/badge/Homebrew-uchebnick%2Ftap-FBB040?logo=homebrew&logoColor=white)](https://github.com/uchebnick/homebrew-tap)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/uchebnick/unch/gh-pages/coverage/coverage-badge.json)](https://github.com/uchebnick/unch/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://github.com/uchebnick/unch)

Local-first CLI for semantic code search over explicit repository annotations. `unch` indexes `@search:` and `@filectx:` comments, enriches them with nearby code, and lets you search the result locally or publish it through GitHub Actions.

## Install

### Homebrew

```bash
brew install uchebnick/tap/unch
```

### From source

```bash
go build -o unch .
```

## Quick Start

Add annotations to the repository you want to search:

```go
// @filectx: CLI entrypoints and command dispatch.
// @search: Run dispatches to index by default and to search when the first arg is search.
func Run(program string, args []string) error {
    ...
}
```

Build a local index and search it:

```bash
unch index --root .
unch search "command dispatch"
```

First run may download the default model, download local `yzma` runtime libraries, and create `./.semsearch/`.

## Core Commands

### `index`

Build or refresh the local search index for a repository.

```bash
unch index --root .
```

Useful flags:

- `--exclude` skip generated or vendor paths
- `--model` use a custom `.gguf` embedding model
- `--lib` use an existing `yzma` runtime directory

Example:

```bash
unch index --root . --exclude vendor/ --exclude "*.pb.go"
```

### `search`

Search the current index.

```bash
unch search "global model cache"
```

Modes:

- `auto` chooses between semantic and lexical search
- `semantic` favors meaning-based matches
- `lexical` favors exact symbol and text matches

Useful flags:

- `--mode`
- `--limit`
- `--max-distance`

Examples:

```bash
unch search "sqlite schema"
unch search --mode lexical "Run"
```

## Remote / CI

Remote indexing is optional. Use it when you want GitHub Actions to publish a search index for the repository.

Create the workflow scaffold:

```bash
unch create ci
```

Bind the repository to a GitHub repo or workflow URL:

```bash
unch bind ci https://github.com/uchebnick/unch
```

After the workflow is committed and runs successfully once, `unch search` can refresh the published index automatically when a newer remote version exists. Use `unch remote sync` to force a refresh before searching.

Set `SEMSEARCH_HOME` if you want to move the shared model cache to a different location.
