#!/usr/bin/env python3

from __future__ import annotations

import json
import sys
from pathlib import Path

from render_benchmark_summary import (
    configure_stream_encoding,
    format_duration_ms,
    format_mode_counts,
)


def collect_report_paths(args: list[str]) -> list[Path]:
    report_paths: list[Path] = []
    seen: set[Path] = set()

    for raw_path in args:
        path = Path(raw_path)
        candidates: list[Path]
        if path.is_dir():
            candidates = sorted(path.glob("**/ci-benchmark.json"))
        else:
            candidates = [path]

        for candidate in candidates:
            resolved = candidate.resolve()
            if resolved in seen or not candidate.is_file():
                continue
            seen.add(resolved)
            report_paths.append(candidate)

    return report_paths


def platform_sort_key(report: dict) -> tuple[int, str, str]:
    env = report["environment"]
    order = {
        ("linux", "amd64"): 0,
        ("linux", "arm64"): 1,
        ("darwin", "arm64"): 2,
        ("darwin", "amd64"): 3,
        ("windows", "amd64"): 4,
        ("windows", "arm64"): 5,
    }
    key = (env["os"], env["arch"])
    return (order.get(key, 99), env["os"], env["arch"])


def render_matrix_summary(reports: list[dict]) -> str:
    lines: list[str] = ["## Benchmark Matrix", ""]

    if not reports:
        lines.append("No benchmark reports were found.")
        lines.append("")
        return "\n".join(lines)

    suites = sorted({report["suite"]["name"] for report in reports})
    revisions = sorted({report["suite_revision"] for report in reports})
    lines.append(f"- Suites: `{', '.join(suites)}`")
    if len(revisions) == 1:
        lines.append(f"- Suite revision: `{revisions[0]}`")
    else:
        lines.append(f"- Suite revisions: `{', '.join(revisions)}`")
    lines.append(f"- Platforms: `{len(reports)}`")
    lines.append("")
    lines.append("### Platform Overview")
    lines.append("")
    lines.append("| Platform | CPU | Tool | Repos | Queries | Modes | Cold index | Warm index | Warm search | Score |")
    lines.append("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")

    for report in reports:
        env = report["environment"]
        coverage = report.get("coverage") or {}
        timing = report["timing"]
        metrics = report["metrics"]
        lines.append(
            f"| `{env['os']}/{env['arch']}` | "
            f"`{env.get('cpu_info') or 'unknown CPU'}` | "
            f"`{env['tool_version']}` | "
            f"`{coverage.get('repository_count', len(report['repositories']))}` | "
            f"`{coverage.get('query_count', 0)}` | "
            f"`{format_mode_counts(coverage.get('mode_counts'))}` | "
            f"`{format_duration_ms(timing['cold_index_mean_ms'])}` | "
            f"`{format_duration_ms(timing['warm_index_mean_ms'])}` | "
            f"`{format_duration_ms(timing['warm_search_mean_ms'])}` | "
            f"`{metrics['quality_score']}` |"
        )

    lines.append("")
    return "\n".join(lines)


def main() -> int:
    configure_stream_encoding()

    if len(sys.argv) < 2:
        print("usage: render_benchmark_matrix_summary.py <report-dir-or-json> [...]", file=sys.stderr)
        return 2

    report_paths = collect_report_paths(sys.argv[1:])
    reports = [json.loads(path.read_text(encoding="utf-8")) for path in report_paths]
    reports.sort(key=platform_sort_key)
    sys.stdout.write(render_matrix_summary(reports))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
