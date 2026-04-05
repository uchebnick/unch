#!/usr/bin/env bash

set -euo pipefail

src_dir="${1:?source directory is required}"
dst_dir="${2:?destination directory is required}"

if [[ ! -d "$src_dir" ]]; then
  echo "source directory not found: $src_dir" >&2
  exit 1
fi

if [[ ! -d "$dst_dir" ]]; then
  echo "destination directory not found: $dst_dir" >&2
  exit 1
fi

rsync -a --delete \
  --exclude '.git' \
  --exclude 'node_modules' \
  --exclude '.mint' \
  --exclude '.DS_Store' \
  "$src_dir"/ "$dst_dir"/
