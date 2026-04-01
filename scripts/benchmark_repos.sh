#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bench_root="${UNCH_BENCH_ROOT:-/tmp/unch-bench}"
cache_root="${UNCH_BENCH_CACHE:-${bench_root}/cache}"
repos_root="${bench_root}/repos"
bin_dir="${bench_root}/bin"
bin_path="${bin_dir}/unch"
warm_root="${bench_root}/warmup"

mkdir -p "${repos_root}" "${bin_dir}" "${cache_root}"

export SEMSEARCH_HOME="${cache_root}/semsearch-home"
export GOMODCACHE="${cache_root}/gomodcache"
export GOCACHE="${cache_root}/gocache"
mkdir -p "${SEMSEARCH_HOME}"
mkdir -p "${GOMODCACHE}" "${GOCACHE}"

shared_model_path="${SEMSEARCH_HOME}/models/embeddinggemma-300m.gguf"
shared_lib_path="${warm_root}/.semsearch/yzma"

measure_seconds() {
  python3 - <<'PY'
import time
print(f"{time.time():.6f}")
PY
}

elapsed_seconds() {
  python3 - "$1" "$2" <<'PY'
import sys
start = float(sys.argv[1])
end = float(sys.argv[2])
print(f"{end - start:.2f}s")
PY
}

clone_or_update() {
  local url="$1"
  local dir="$2"

  if [ -d "${dir}/.git" ]; then
    git -C "${dir}" fetch --depth 1 origin
    local head
    head="$(git -C "${dir}" symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null || true)"
    if [ -n "${head}" ]; then
      head="${head#origin/}"
      git -C "${dir}" checkout -q "${head}"
      git -C "${dir}" reset --hard "origin/${head}" >/dev/null
    else
      git -C "${dir}" reset --hard origin/main >/dev/null 2>&1 || git -C "${dir}" reset --hard origin/master >/dev/null
    fi
    return
  fi

  git clone --depth 1 "${url}" "${dir}"
}

run_query() {
  local root="$1"
  local query="$2"
  shift 2
  local output
  if [ "$#" -gt 0 ]; then
    output="$("${bin_path}" search --root "${root}" --limit 1 --model "${shared_model_path}" --lib "${shared_lib_path}" "$@" "${query}" 2>&1)"
  else
    output="$("${bin_path}" search --root "${root}" --limit 1 --model "${shared_model_path}" --lib "${shared_lib_path}" "${query}" 2>&1)"
  fi
  local top
  top="$(printf '%s\n' "${output}" | tr '\r' '\n' | grep -E '^[[:space:]]*1\.' | head -1 | sed 's/^[[:space:]]*//' | sed 's/[[:space:]]*$//')"
  printf '%s' "${top}"
}

index_repo() {
  local root="$1"
  rm -rf "${root}/.semsearch"

  local start end output summary duration
  start="$(measure_seconds)"
  output="$("${bin_path}" index --root "${root}" --model "${shared_model_path}" --lib "${shared_lib_path}" 2>&1)"
  end="$(measure_seconds)"
  duration="$(elapsed_seconds "${start}" "${end}")"
  summary="$(printf '%s\n' "${output}" | tr '\r' '\n' | grep 'Indexed ' | tail -1 | sed 's/[[:space:]]*$//')"
  printf '%s|%s' "${summary}" "${duration}"
}

warm_up_runtime() {
  mkdir -p "${warm_root}"
  cat > "${warm_root}/warmup.go" <<'EOF'
package warmup

// Hello returns a small symbol for cache warmup.
func Hello() string { return "hello" }
EOF
  "${bin_path}" index --root "${warm_root}" >/dev/null 2>&1
}

benchmark_repo() {
  local name="$1"
  local url="$2"
  local dir="${repos_root}/${name}"
  shift 2
  local queries=("$@")

  clone_or_update "${url}" "${dir}"
  local result summary duration
  result="$(index_repo "${dir}")"
  summary="${result%%|*}"
  duration="${result##*|}"

  echo "## ${name}"
  echo
  echo "- Source: ${url}"
  echo "- ${summary}"
  echo "- Index time: ${duration}"
  echo
  echo "| Query | Top result |"
  echo "| --- | --- |"

  local query mode top
  for query in "${queries[@]}"; do
    mode=""
    if [[ "${query}" == lexical:* ]]; then
      mode="--mode lexical"
      query="${query#lexical:}"
    fi
    # shellcheck disable=SC2086
    top="$(run_query "${dir}" "${query}" ${mode})"
    echo "| \`${query}\` | \`${top}\` |"
  done
  echo
}

echo "# unch benchmark snapshot"
echo
echo "- Generated from current checkout"
echo "- Benchmark root: \`${bench_root}\`"
echo "- Warm model/runtime cache: yes"
echo

go build -buildvcs=false -o "${bin_path}" "${repo_root}"
warm_up_runtime

benchmark_repo \
  "gorilla/mux" \
  "https://github.com/gorilla/mux.git" \
  "create a new router" \
  "get path variables from a request"

benchmark_repo \
  "spf13/cobra" \
  "https://github.com/spf13/cobra.git" \
  "add a subcommand" \
  "lexical:ExecuteC"
