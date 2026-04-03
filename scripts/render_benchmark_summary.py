#!/usr/bin/env python3

from __future__ import annotations

import json
import sys
from pathlib import Path


def format_duration_ms(value: float) -> str:
    if value >= 1000:
        return f"{value / 1000:.2f}s"
    return f"{value:.2f}ms"


def format_mode_counts(counts: dict | None) -> str:
    if not counts:
        return "none"

    ordered_keys = ["auto", "semantic", "lexical"]
    parts: list[str] = []
    seen: set[str] = set()

    for key in ordered_keys:
        if key in counts:
            parts.append(f"{key}={counts[key]}")
            seen.add(key)

    for key in sorted(counts):
        if key in seen:
            continue
        parts.append(f"{key}={counts[key]}")

    return " ".join(parts)


def pluralize(value: int, singular: str, plural: str | None = None) -> str:
    if value == 1:
        return singular
    return plural or f"{singular}s"


def safe_top_hit(query: dict) -> str:
    top_hit = query.get("top_hit")
    if top_hit:
        observed_rank = query.get("metrics", {}).get("observed_rank") or 0
        if observed_rank:
            return f"`{top_hit['path']}:{top_hit['line']}` @ rank `{observed_rank}`"
        return f"`{top_hit['path']}:{top_hit['line']}`"

    runs = query.get("runs") or []
    if not runs:
        return "No hits"
    hits = runs[0].get("hits") or []
    if not hits:
        return "No hits"
    hit = hits[0]
    return f"`{hit['path']}:{hit['line']}`"


def render_summary(report: dict) -> str:
    lines: list[str] = []
    env = report["environment"]
    timing = report["timing"]
    metrics = report["metrics"]
    coverage = report.get("coverage") or {}
    config = report.get("config") or {}

    lines.append("## Benchmark")
    lines.append("")
    lines.append(f"- Tool: `{report['tool']}` (`{env['tool_version']}`)")
    lines.append(f"- Suite: `{report['suite']['name']}`")
    lines.append(f"- Suite revision: `{report['suite_revision']}`")
    lines.append(f"- Environment: `{env['os']}/{env['arch']}` • `{env.get('cpu_info') or 'unknown CPU'}` • `{env['num_cpu']} cores`")
    if coverage:
        repository_count = coverage.get("repository_count", len(report["repositories"]))
        query_count = coverage.get("query_count", 0)
        lines.append(
            f"- Suite coverage: `{repository_count} {pluralize(repository_count, 'repo')}` • "
            f"`{query_count} {pluralize(query_count, 'query', 'queries')}` • "
            f"`{format_mode_counts(coverage.get('mode_counts'))}`"
        )
    if config:
        lines.append(
            f"- Run profile: `{config.get('cold_index_runs', 0)} cold / {config.get('warm_index_runs', 0)} warm / "
            f"{config.get('warm_search_runs', 0)} search repeats` • top `{config.get('search_limit', 0)}` hits"
        )
    lines.append(f"- Cold index mean: `{format_duration_ms(timing['cold_index_mean_ms'])}`")
    lines.append(f"- Warm index mean: `{format_duration_ms(timing['warm_index_mean_ms'])}`")
    lines.append(f"- Warm search mean: `{format_duration_ms(timing['warm_search_mean_ms'])}`")
    lines.append(
        f"- Quality: `{metrics['quality_score']}/100` "
        f"(top1=`{metrics['top1']:.3f}` top3=`{metrics['top3']:.3f}` mrr=`{metrics['mrr']:.3f}`)"
    )
    lines.append("")
    lines.append("### Repositories")
    lines.append("")
    lines.append("| Repo | Language | Queries | Modes | Latest index | Cold index | Warm index | Warm search | Score |")
    lines.append("| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
    for repo in report["repositories"]:
        repo_timing = repo["timing"]
        repo_metrics = repo["metrics"]
        repo_stats = repo.get("stats") or {}
        latest_index = "n/a"
        if repo_stats.get("last_indexed_symbols") or repo_stats.get("last_indexed_files"):
            latest_index = f"{repo_stats.get('last_indexed_symbols', 0)} symbols / {repo_stats.get('last_indexed_files', 0)} files"
        lines.append(
            f"| `{repo['id']}` | `{repo['language']}` | "
            f"`{repo_stats.get('query_count', len(repo.get('queries') or []))}` | "
            f"`{format_mode_counts(repo_stats.get('mode_counts'))}` | "
            f"`{latest_index}` | "
            f"`{format_duration_ms(repo_timing['cold_index_mean_ms'])}` | "
            f"`{format_duration_ms(repo_timing['warm_index_mean_ms'])}` | "
            f"`{format_duration_ms(repo_timing['warm_search_mean_ms'])}` | "
            f"`{repo_metrics['quality_score']}` |"
        )

    misses = []
    for repo in report["repositories"]:
        for query in repo["queries"]:
            query_metrics = query["metrics"]
            if query_metrics["top1_success"]:
                continue
            misses.append((repo, query))

    lines.append("")
    lines.append("### Ranking Misses")
    lines.append("")
    if not misses:
        lines.append("All benchmark queries returned an expected exact hit at rank 1.")
    else:
        for repo, query in misses:
            query_metrics = query["metrics"]
            query_timing = query.get("timing") or {}
            lines.append(
                f"- `{repo['id']}` / `{query['id']}`: expected "
                f"`{', '.join(query['expected_hits'])}`, got {safe_top_hit(query)} "
                f"(mode=`{query.get('mode', 'unknown')}` search=`{format_duration_ms(query_timing.get('warm_search_mean_ms', 0))}` "
                f"top3=`{'yes' if query_metrics['top3_success'] else 'no'}` rr=`{query_metrics['rr']:.3f}`)"
            )

    slowest_queries = []
    for repo in report["repositories"]:
        for query in repo.get("queries") or []:
            slowest_queries.append((repo["id"], query))
    slowest_queries.sort(key=lambda item: (item[1].get("timing") or {}).get("warm_search_mean_ms", 0), reverse=True)

    lines.append("")
    lines.append("### Slowest Queries")
    lines.append("")
    if not slowest_queries:
        lines.append("No query timings recorded.")
    else:
        for repo_id, query in slowest_queries[:10]:
            query_timing = query.get("timing") or {}
            lines.append(
                f"- `{repo_id}` / `{query['id']}`: `{format_duration_ms(query_timing.get('warm_search_mean_ms', 0))}` "
                f"({query.get('mode', 'unknown')}) → {safe_top_hit(query)}"
            )

    lines.append("")
    lines.append("### Artifact")
    lines.append("")
    lines.append("Machine-readable report uploaded as `benchmark-report`.")
    lines.append("")

    return "\n".join(lines)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: render_benchmark_summary.py <report.json>", file=sys.stderr)
        return 2

    report_path = Path(sys.argv[1])
    report = json.loads(report_path.read_text())
    sys.stdout.write(render_summary(report) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
