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
  -v VERSION   version tag to install, for example v0.3.0
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
    MINGW*|MSYS*|CYGWIN*|Windows_NT) printf 'Windows\n' ;;
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

has_system_linux_loader() {
  case "$(detect_arch)" in
    x86_64) [ -e /lib64/ld-linux-x86-64.so.2 ] ;;
    arm64) [ -e /lib/ld-linux-aarch64.so.1 ] ;;
    *) return 1 ;;
  esac
}

needs_nix_loader_patch() {
  [ "$(detect_os)" = "Linux" ] && has_cmd nix && ! has_system_linux_loader
}

patch_nixos_binary() {
  binary_path="$1"

  if ! has_cmd nix-shell; then
    return 1
  fi

  BINARY_PATH="$binary_path" nix-shell -p patchelf stdenv.cc --run '
      set -eu
      linker="$(cat "$NIX_CC/nix-support/dynamic-linker")"
      glibc_dir="$(dirname "$linker")"
      libgcc_dir="$(dirname "$(cc -print-file-name=libgcc_s.so.1)")"
      patchelf --set-interpreter "$linker" --set-rpath "${glibc_dir}:${libgcc_dir}" "$BINARY_PATH"
    '
}

install_unix_binary() {
  source_path="$1"
  destination_path="$2"

  install -m 0755 "$source_path" "$destination_path"

  if needs_nix_loader_patch; then
    say "Detected a Linux environment without a system ELF loader; patching ${destination_path} via nix"
    patch_nixos_binary "$destination_path"
  fi
}

install_release_archive() {
  version="$1"
  os_name="$2"
  arch_name="$3"

  case "$os_name" in
    Windows)
      archive_ext="zip"
      asset_binary="unch.exe"
      ;;
    *)
      archive_ext="tar.gz"
      asset_binary="unch"
      ;;
  esac

  if ! has_cmd curl; then
    return 1
  fi

  if [ "$archive_ext" = "tar.gz" ] && ! has_cmd tar; then
    return 1
  fi
  if [ "$archive_ext" = "zip" ] && ! has_cmd unzip; then
    return 1
  fi

  asset="unch_${os_name}_${arch_name}.${archive_ext}"
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT INT TERM HUP
  asset_path="${tmp_dir}/${asset}"

  if [ -n "${UNCH_INSTALL_ASSET_DIR:-}" ] && [ -f "${UNCH_INSTALL_ASSET_DIR}/${asset}" ]; then
    say "Using local install asset ${UNCH_INSTALL_ASSET_DIR}/${asset}"
    cp "${UNCH_INSTALL_ASSET_DIR}/${asset}" "${asset_path}"
  else
    url="https://github.com/${repo}/releases/download/${version}/${asset}"
    say "Downloading ${url}"
    if ! curl -fsSL "$url" -o "${asset_path}"; then
      return 1
    fi
  fi

  case "$archive_ext" in
    tar.gz)
      tar -xzf "${asset_path}" -C "${tmp_dir}"
      install_unix_binary "${tmp_dir}/${asset_binary}" "${bin_dir}/${asset_binary}"
      ;;
    zip)
      unzip -q "${asset_path}" -d "${tmp_dir}"
      install -m 0755 "${tmp_dir}/${asset_binary}" "${bin_dir}/${asset_binary}"
      ;;
  esac
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

if [ "$version" != "latest" ] || [ -n "${UNCH_INSTALL_ASSET_DIR:-}" ]; then
  if install_release_archive "$version" "$os_name" "$arch_name"; then
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
  say "Release archives are currently published for Darwin arm64/x86_64, Linux arm64/x86_64, and Windows arm64/x86_64."
  say "Install Go and rerun this script, or use Homebrew on macOS."
  exit 1
fi

installed_binary="unch"
if [ "$os_name" = "Windows" ]; then
  installed_binary="unch.exe"
fi

say "Installed unch to ${bin_dir}/${installed_binary}"
case ":$PATH:" in
  *":${bin_dir}:"*) ;;
  *)
    say "Note: ${bin_dir} is not currently on PATH."
    ;;
esac
