# Benchmarks

`unch` ships with a versioned benchmark suite and an end-to-end CLI runner.

The runner answers three practical questions:

1. How long does indexing take?
2. How long does search take?
3. How often does the tool return the exact expected `path:line`?

It measures real `unch index` and `unch search` subprocesses, not internal Go APIs.

## Benchmark Suites

The checked-in suites now have explicit identity and version fields:

- `suite_id`
- `suite_version`

This is important because benchmark results are only directly comparable when they come from the same suite definition.

### Smoke Suite

File: [`benchmarks/suites/smoke.json`](../benchmarks/suites/smoke.json)

- `suite_id`: `smoke`
- `suite_version`: `1`
- size: `12` queries
- purpose: fast validation in CI

### Default Suite

File: [`benchmarks/suites/default.json`](../benchmarks/suites/default.json)

- `suite_id`: `default`
- `suite_version`: `3`
- size: `161` queries across `8` pinned repositories
- purpose: broader tool-to-tool comparisons and quality tracking

## Quick Start

Run the full default suite:

```bash
go run ./cmd/bench
```

Run the smaller smoke suite:

```bash
go run ./cmd/bench -suite ./benchmarks/suites/smoke.json
```

The shell wrapper forwards directly to the Go runner:

```bash
scripts/benchmark_repos.sh
```

Write the JSON report to a specific path:

```bash
go run ./cmd/bench \
  -suite ./benchmarks/suites/default.json \
  -output ./benchmarks/results/unch-local.json
```

Pass a custom model or `yzma` installation:

```bash
go run ./cmd/bench \
  -tool-option model=/path/to/model.gguf \
  -tool-option lib=/path/to/yzma
```

Use a known model id instead of a full path:

```bash
go run ./cmd/bench \
  -tool-option model=qwen3
```

## What The Runner Measures

The runner records:

- `cold index mean`
- `warm index mean`
- `warm search mean`
- per-query `warm search mean`
- per-query top hit and observed rank
- `top1`
- `top3`
- `mrr`
- `quality score`
- suite coverage and per-repository mode mix

### Timing Definitions

`cold index`

- one index run on a repo with its local `.semsearch/` removed
- model/runtime caches are already present
- embeddings are recomputed because the local index database starts empty
- network download time is not included

`warm index`

- repeated index runs on the same pinned checkout
- shared model/runtime caches stay warm
- the existing local `.semsearch/` directory is kept between repeats
- stored embedding hashes can be reused, so unchanged symbols should skip model inference on cache hit

`warm search`

- repeated searches against an already-built local index
- averaged per query and then per repository / per tool

## Quality Scoring

Each query defines one or more acceptable exact hits:

```json
{
  "id": "new-router-semantic",
  "text": "create a new router",
  "mode": "auto",
  "expected_hits": ["mux.go:32"]
}
```

The runner scores ranked output against those exact targets:

- `top1`: expected hit is ranked first
- `top3`: expected hit appears in the first three results
- `mrr`: reciprocal rank of the first expected hit in the top 10

Composite score:

```text
score = round(100 * (0.5 * top1 + 0.2 * top3 + 0.3 * mrr))
```

This score is intentionally strict. It measures exact symbol localization, not vague semantic similarity.

## Query Matrix

The source of truth for benchmark cases is the suite JSON itself.

Today the default suite covers:

- `gorilla/mux`: `39` queries
- `developit/mitt`: `28` queries
- `expressjs/morgan`: `31` queries
- `pallets-eco/blinker`: `31` queries
- `go-chi/chi`: `8` queries
- `sindresorhus/p-queue`: `8` queries
- `expressjs/cors`: `8` queries
- `theskumar/python-dotenv`: `8` queries

The cases are a mix of:

- explicit `semantic` queries
- `auto` natural-language queries
- lexical symbol-name queries
- paraphrases that hit the same expected `path:line`

That larger matrix is deliberate. It makes the suite more resistant to “one lucky query phrasing” and gives better signal when ranking changes.

## How To Read The Output

Typical summary:

```text
Tool: unch (v0.3.0)
Suite: /.../benchmarks/suites/smoke.json [smoke v1]
Suite revision: sha256:...
Environment: darwin/arm64 • Apple M4 • 10 cores
Suite coverage: 4 repos • 12 queries • auto=8 lexical=4
Run profile: 1 cold / 3 warm / 5 search repeats • top 10 hits
Cold index mean: 1.95s
Warm index mean: 316.68ms
Warm search mean: 305.57ms
Quality: 95/100 (top1=0.917 top3=1.000 mrr=0.958)
```

Interpretation:

- `cold index mean` tells you rebuild cost from empty local index state
- `warm index mean` tells you rebuild cost once caches are ready
- `warm search mean` is the user-facing query latency
- `top1` shows how often the first answer is exactly right
- `top3` shows whether the right answer still stays near the top
- `mrr` punishes rank drift
- `quality score` is the compact comparison number, but it should always be read together with the raw metrics
- `suite coverage` and `run profile` make it explicit how broad the run was and how many repeats were used
- `latest index snapshot` per repo tells you roughly how much code was indexed in the most recent successful run
- `top1 misses` and the GitHub summary's `slowest queries` section make it easier to see whether regressions came from ranking drift, latency spikes, or both

The release-tag GitHub Actions benchmark uses the smoke suite with a lighter `1 cold / 1 warm / 1 search repeat` profile so release CI stays reasonably fast. Local runs keep the heavier defaults unless you override them with flags.

## Result Files

Each run writes machine-readable JSON under `benchmarks/results/`.

The report contains:

- benchmark environment
- suite path and suite revision hash
- suite metadata including `suite_id` and `suite_version`
- suite coverage including repo count, query count, and mode mix
- per-repository timing and quality breakdown
- per-repository query count, mode mix, and latest indexed symbol/file counts
- per-query hits, top hit, observed rank, and mean search timing

That JSON is the source of truth for comparisons.

## Governance

Changing a suite is meaningful.

At minimum:

- changing queries
- changing expected `path:line`
- changing pinned commits
- adding or removing repositories

should be treated as a suite change, not as “the same benchmark”.

Rule of thumb:

- small text cleanup with identical semantics: keep the version
- meaningful benchmark behavior change: bump `suite_version`

Results from different suite versions should not be compared as if they were the same benchmark.
