# Benchmarks

This page tracks a small reproducible benchmark snapshot for `unch`.

It is intentionally a smoke benchmark, not a comprehensive evaluation. The goal is to show:

- index time on small real repositories
- the size of the indexed symbol set
- a couple of representative search queries with their top match

The benchmark harness lives in [`scripts/benchmark_repos.sh`](../scripts/benchmark_repos.sh) and benchmarks the current checkout.

## Methodology

- build the current checkout locally
- warm model and runtime downloads before measuring
- benchmark two small public repositories
- run one or two representative semantic and lexical smoke queries per repository
- record top result path:line for each query

These numbers are machine-dependent and should be treated as comparative smoke data, not strict performance guarantees.

## Current Snapshot

Generated from `scripts/benchmark_repos.sh` with warm model and runtime caches.

### `gorilla/mux`

- Source: [`github.com/gorilla/mux`](https://github.com/gorilla/mux)
- Index summary: `Indexed 278 symbols in 16 files`
- Index time: `5.66s`

| Query | Top result |
| --- | --- |
| `create a new router` | `mux.go:32` |
| `get path variables from a request` | `mux.go:466` |

### `spf13/cobra`

- Source: [`github.com/spf13/cobra`](https://github.com/spf13/cobra)
- Index summary: `Indexed 677 symbols in 36 files`
- Index time: `12.57s`

| Query | Top result |
| --- | --- |
| `add a subcommand` | `command.go:1205` |
| `ExecuteC` | `command.go:269` |

## Repositories

- [`gorilla/mux`](https://github.com/gorilla/mux)
- [`spf13/cobra`](https://github.com/spf13/cobra)

## Notes

- The benchmark script uses a dedicated cache root so repeated runs behave like warm local usage.
- Query quality examples are included to show that the tool is finding the intended symbols, not just producing timings.
- The exact score or mode label may vary between releases, but the benchmark is considered healthy when the top result still lands on the intended symbol definition.
