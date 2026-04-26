#!/usr/bin/env bash

set -euo pipefail

pkg="${1:?usage: install_msys2_package.sh <package>}"
attempts="${MSYS2_PACMAN_ATTEMPTS:-3}"
base_delay="${MSYS2_PACMAN_RETRY_DELAY_SECONDS:-5}"

attempt=1
while [ "$attempt" -le "$attempts" ]; do
  if pacman --noconfirm -S --needed --overwrite '*' "$pkg"; then
    exit 0
  fi

  if [ "$attempt" -eq "$attempts" ]; then
    echo "pacman install failed for ${pkg} after ${attempts} attempts" >&2
    exit 1
  fi

  echo "Retrying pacman install for ${pkg} (attempt $((attempt + 1))/${attempts})" >&2
  pacman -Scc --noconfirm || true
  sleep $((base_delay * attempt))
  attempt=$((attempt + 1))
done
