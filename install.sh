#!/usr/bin/env sh

set -eu

repo="uchebnick/unch"
bin_dir="${HOME}/.local/bin"
requested_version=""

usage() {
  cat <<'EOF'
Usage: install.sh [-b BIN_DIR] [-v VERSION]

Installs unch into the selected bin directory.

Options:
  -b BIN_DIR   install destination (default: $HOME/.local/bin)
  -v VERSION   version tag to install, for example v0.2.1
  -h           show this help
EOF
}

say() {
  printf '%s\n' "$*" >&2
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

normalize_version() {
  version="$1"
  if [ -z "$version" ] || [ "$version" = "latest" ]; then
    printf 'latest\n'
    return
  fi
  case "$version" in
    v*) printf '%s\n' "$version" ;;
    *) printf 'v%s\n' "$version" ;;
  esac
}

resolve_latest_version() {
  if ! has_cmd curl; then
    printf 'latest\n'
    return
  fi

  effective_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${repo}/releases/latest" 2>/dev/null || true)"
  tag="${effective_url##*/}"
  case "$tag" in
    ""|latest) printf 'latest\n' ;;
    *) printf '%s\n' "$tag" ;;
  esac
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'Darwin\n' ;;
    Linux) printf 'Linux\n' ;;
    *) printf 'unknown\n' ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64\n' ;;
    x86_64|amd64) printf 'x86_64\n' ;;
    *) printf 'unknown\n' ;;
  esac
}

install_release_archive() {
  version="$1"
  os_name="$2"
  arch_name="$3"

  if ! has_cmd curl || ! has_cmd tar; then
    return 1
  fi

  asset="unch_${os_name}_${arch_name}.tar.gz"
  url="https://github.com/${repo}/releases/download/${version}/${asset}"
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT INT TERM HUP

  say "Downloading ${url}"
  if ! curl -fsSL "$url" -o "${tmp_dir}/${asset}"; then
    return 1
  fi

  tar -xzf "${tmp_dir}/${asset}" -C "${tmp_dir}"
  install -m 0755 "${tmp_dir}/unch" "${bin_dir}/unch"
  rm -rf "$tmp_dir"
  trap - EXIT INT TERM HUP
  return 0
}

install_with_go() {
  version="$1"

  if ! has_cmd go; then
    return 1
  fi

  if [ "$version" = "latest" ]; then
    pkg_version='@latest'
  else
    pkg_version="@${version}"
  fi

  say "Installing via go install github.com/${repo}${pkg_version}"
  GOBIN="${bin_dir}" go install "github.com/${repo}${pkg_version}"
}

while getopts "b:v:h" opt; do
  case "$opt" in
    b) bin_dir="$OPTARG" ;;
    v) requested_version="$OPTARG" ;;
    h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
done

mkdir -p "$bin_dir"

version="$(normalize_version "$requested_version")"
if [ "$version" = "latest" ]; then
  version="$(resolve_latest_version)"
fi

os_name="$(detect_os)"
arch_name="$(detect_arch)"

installed="false"

if [ "$os_name" = "Darwin" ] && { [ "$arch_name" = "arm64" ] || [ "$arch_name" = "x86_64" ]; }; then
  if [ "$version" != "latest" ] && install_release_archive "$version" "$os_name" "$arch_name"; then
    installed="true"
  fi
fi

if [ "$installed" != "true" ]; then
  if install_with_go "$version"; then
    installed="true"
  fi
fi

if [ "$installed" != "true" ]; then
  say "Could not install unch for ${os_name}/${arch_name}."
  say "Release archives are currently published for Darwin arm64 and x86_64."
  say "Install Go and rerun this script, or use Homebrew on macOS."
  exit 1
fi

say "Installed unch to ${bin_dir}/unch"
case ":$PATH:" in
  *":${bin_dir}:"*) ;;
  *)
    say "Note: ${bin_dir} is not currently on PATH."
    ;;
esac
