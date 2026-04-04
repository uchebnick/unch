# Compatibility

This document describes the current compatibility contract for `unch`.

## Compatibility Layers

### Module Path Compatibility

- The canonical Go module path is `github.com/uchebnick/unch`.
- New source-based installs should use `go install github.com/uchebnick/unch@latest`.
- Older references to `github.com/uchebnick/unch-searcher` should be treated as legacy and updated rather than relied on through repository redirects.

### CLI Compatibility

- Command names and existing flags are expected to remain stable within patch releases.
- Breaking CLI changes should only happen in minor releases and must be called out in release notes.
- New flags are additive and should not change the meaning of existing commands silently.

### Manifest Compatibility

- Local repository state lives in `.semsearch/manifest.json`.
- Manifest compatibility is governed by `schema_version`.
- `version` is the logical publication or generation number of the indexed repository state.
- `version` is not the same thing as the index database schema version.
- Remote binding metadata also lives in the manifest, which means remote configuration is local checkout state, not repository source history.

### Index Database Compatibility

- `index.db` is a rebuildable cache artifact, not a durable user database.
- Compatibility is determined by the schema and query expectations of the running `unch` binary.
- Active index state is model-scoped: each embedding model family keeps its own active snapshot.
- When the current binary cannot use a database layout, the correct fix is to rebuild the index, not to migrate user data in place.
- Local upgrades may therefore require `unch index` after releases that change indexing or storage behavior.

## Remote and CI Compatibility

- Remote sync trusts the published index only when it is compatible with the local binary’s expectations.
- If a published remote index uses an older incompatible schema, local search should keep using a compatible local cache when available.
- Repositories that use remote indexing must rerun the `searcher` workflow after incompatible indexing releases so CI republishes a compatible `index.db` and `manifest.json`.
- A local reindex detaches remote binding and returns the manifest to `source: "local"` until the repository is rebound.

## Upgrade Notes

- Patch releases aim to preserve CLI behavior and avoid index schema breaks.
- Minor releases may require:
  - a local `unch index`
  - one successful CI republish for repositories that use remote index sync
- Release notes are the source of truth for upgrade-impacting behavior.

## Support Matrix

| Area | Status | Notes |
| --- | --- | --- |
| Go indexing | Supported | Tree-sitter-based symbol extraction |
| TypeScript indexing | Supported | Tree-sitter-based symbol extraction |
| JavaScript indexing | Supported | Tree-sitter-based symbol extraction |
| Python indexing | Supported | Tree-sitter-based symbol extraction |
| Other languages | Limited | Legacy prefix fallback only |
| Search modes | Supported | `auto`, `semantic`, `lexical` |
| Homebrew install | Supported | macOS-first polished install path |
| `go install` | Supported | Canonical module path is `github.com/uchebnick/unch` |
| `install.sh` | Supported | Uses release assets on macOS and Linux by default, with Go fallback for unsupported targets |
| `install/install.ps1` | Supported | Uses release assets on Windows by default, with Go fallback elsewhere |
| Darwin release binaries | Supported | `arm64` and `x86_64` |
| Linux release binaries | Supported | `arm64` and `x86_64` |
| Windows release binaries | Supported | `arm64` and `x86_64` (`unch.exe`) |
| Remote indexing | Supported | GitHub Actions `searcher` workflow |

Published release binaries and CI builds on macOS, Linux, and Windows arm64/x86_64 use the full cgo-backed Tree-sitter and SQLite stack. Source builds on supported cgo toolchains do not require a separately installed SQLite development package because the SQLite header used by the embedded `sqlite-vec` bridge is vendored in-tree. Manual Windows builds without cgo remain a fallback path and should not be treated as identical to the published binaries.

## Current Practical Rules

- If `unch` upgrades but your local search breaks, rebuild with `unch index`.
- If remote sync reports an incompatible published schema, rerun the repository’s `searcher` workflow.
- If you automate installation from source, use `go install github.com/uchebnick/unch@latest`.
