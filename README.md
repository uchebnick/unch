# unch

[![Release](https://img.shields.io/github/v/release/uchebnick/unch-searcher?display_name=tag)](https://github.com/uchebnick/unch-searcher/releases/latest)
[![Homebrew Tap](https://img.shields.io/badge/Homebrew-uchebnick%2Ftap-FBB040?logo=homebrew&logoColor=white)](https://github.com/uchebnick/homebrew-tap)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/uchebnick/unch-searcher/gh-pages/coverage/coverage-badge.json)](https://github.com/uchebnick/unch-searcher/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](#build-from-source)
[![SQLite vec](https://img.shields.io/badge/SQLite-sqlite--vec-003B57?logo=sqlite&logoColor=white)](#how-it-works)
[![Local First](https://img.shields.io/badge/Local-first-2DA44E)](#storage-layout)

Local-first CLI for semantic code search over explicit repository annotations.

`unch` indexes `@search:` and `@filectx:` comments, enriches them with the next 10 lines of code, embeds them with a local GGUF model, and stores everything in SQLite via `sqlite-vec`.

## Install

### Homebrew

```bash
brew install uchebnick/tap/unch
```

If Homebrew complains about outdated Xcode on macOS, update Xcode Command Line Tools first:

```bash
softwareupdate --list
softwareupdate --install "Command Line Tools for Xcode 26.4-26.4"
sudo xcode-select --switch /Library/Developer/CommandLineTools
```

If Homebrew still complains about an outdated `Xcode.app`, update the full Xcode app from the App Store as well, or remove the stale app if you only want Command Line Tools.

### Manual download

Prebuilt archives for `v0.1.0`:

- Apple Silicon: `unch_Darwin_arm64.tar.gz`
- Intel Mac: `unch_Darwin_x86_64.tar.gz`

Release page:

https://github.com/uchebnick/unch-searcher/releases/tag/v0.1.0

### Build from source

```bash
go build -o unch .
```

## Quick Start

Index the current repository:

```bash
unch --root .
```

Search the current repository:

```bash
unch search "global model cache"
```

First run may automatically:

- download the default embedding model
- download local `yzma` runtime libraries
- create `./.semsearch/index.db`

## Annotation Syntax

Write annotations in regular code comments.

File-level context:

```go
// @filectx: CLI entrypoint for indexing and semantic search over repository comments stored in sqlite.
```

Searchable note next to a function:

```go
// @search: RunCLI dispatches to index mode by default and to the search subcommand when the first arg is search.
func RunCLI(program string, args []string) error {
    ...
}
```

What gets embedded for each annotation:

- annotation text itself
- collected file context from `@filectx:`
- the next 10 lines after the annotation

`README*` files are skipped from indexing so documentation examples do not pollute search results.

## Usage

### Create CI

Create a local GitHub Actions workflow scaffold:

```bash
unch create ci
```

This writes `./.github/workflows/searcher.yml`. The generated workflow installs `unch`, builds `.semsearch/index.db`, and uploads the search index as a GitHub Actions artifact.

### Index

Default command:

```bash
unch --root .
```

Explicit subcommand:

```bash
unch index --root .
```

Useful flags:

- `--exclude <pattern>` to skip paths or globs
- `--db <path>` to use a custom SQLite file
- `--model <path>` to use a custom `.gguf` model
- `--lib <path>` to use a custom `yzma` runtime directory
- `--comment-prefix "@search:"`
- `--context-prefix "@filectx:"`
- `--gitignore <path>`

Example:

```bash
unch --root . \
  --exclude vendor/ \
  --exclude "*.pb.go"
```

### Search

```bash
unch search "global model cache"
```

Useful flags:

- `--limit 10`
- `--mode auto|semantic|lexical`
- `--max-distance 0.85`
- `--root <path>` to format result paths relative to another root

Examples:

```bash
unch search "sqlite database schema"
unch search --mode lexical "RunCLI"
unch search --mode semantic "yzma runtime libraries"
unch search --mode semantic --max-distance 0.80 "global model cache"
```

## Output

Model download and indexing render a compact progress bar:

```text
Downloading model  [============================] 100% 313.4 MiB/313.4 MiB
Indexing           [============================] 30/30
Indexed 30 comments in 6 files
```

Search output includes only file, line, and ranking metric:

```text
Found 3 matches
 1. internal/runtime/model_cache.go:27  0.8110
 2. internal/runtime/model_cache.go:3   0.8465
 3. internal/cli/root.go:19             lexical
```

- `lexical` means keyword-based ranking won
- numeric values are semantic distances, where lower is better

Detailed runtime logs are written to `./.semsearch/logs/run.log`.

## How It Works

1. Walk the repository.
2. Skip internal state and documentation noise like `.git`, `.semsearch`, and `README*`.
3. Extract `@search:` and `@filectx:` directives from normal source comments.
4. Build an embedding document from annotation text, file context, and the next 10 lines.
5. Store vectors in SQLite through `sqlite-vec`.
6. Search with semantic retrieval, lexical matching, or auto mode.

## Storage Layout

Local project state:

- `./.semsearch/index.db`
- `./.semsearch/logs/run.log`
- `./.semsearch/yzma/`

Global cache:

- macOS default: `~/Library/Caches/unch/models/`

Override the global cache root with `SEMSEARCH_HOME`.

## Model And Runtime

By default the tool uses:

- a GGUF embedding model stored in the global cache
- `yzma` runtime libraries stored per project in `./.semsearch/yzma`

This means:

- the model is reused across repositories
- runtime libraries stay local to each indexed project

## Environment Variables

- `SEMSEARCH_HOME` overrides the global cache root
- `SEMSEARCH_MODEL_URL` overrides the default GGUF model download URL
- `YZMA_LIB` points to an existing `yzma` runtime directory

## Current Limitations

- the index is built from annotations, not from the full AST or every function body
- search quality depends heavily on the quality of `@search:` comments
- broad queries like `database` can still rank imperfectly
- the current sweet spot is intent search and codebase navigation, not full-source replacement for grep

## Verification

```bash
go build ./...
unch --root .
unch search "RunCLI"
unch search --mode semantic "yzma runtime libraries"
unch search "pupa"
```
