#!/usr/bin/env sh

set -eu

repo="uchebnick/unch"
source_pkg="github.com/${repo}/cmd/unch"
bin_dir=""
requested_version=""

usage() {
  cat <<'EOF'
Usage: install.sh [-b BIN_DIR] [-v VERSION]

Installs unch into the selected bin directory.

Options:
  -b BIN_DIR   install destination (default: first writable PATH directory, then $HOME/.local/bin)
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

sha256_file() {
  file_path="$1"

  if has_cmd sha256sum; then
    sha256sum "$file_path" | {
      IFS=' ' read -r checksum _
      printf '%s\n' "$checksum"
    }
    return
  fi

  if has_cmd shasum; then
    shasum -a 256 "$file_path" | {
      IFS=' ' read -r checksum _
      printf '%s\n' "$checksum"
    }
    return
  fi

  if has_cmd openssl; then
    openssl dgst -sha256 "$file_path" | {
      read -r _ checksum
      printf '%s\n' "$checksum"
    }
    return
  fi

  return 1
}

find_expected_checksum() {
  checksums_path="$1"
  asset_name="$2"

  while IFS= read -r line || [ -n "$line" ]; do
    set -- $line
    checksum="${1:-}"
    file_path="${2:-}"
    [ -n "$checksum" ] || continue
    [ -n "$file_path" ] || continue
    file_path="${file_path#\*}"
    file_name="${file_path##*/}"
    if [ "$file_name" = "$asset_name" ]; then
      printf '%s\n' "$checksum"
      return 0
    fi
  done < "$checksums_path"

  return 1
}

resolve_checksums_file() {
  version="$1"
  tmp_dir="$2"

  if [ -n "${UNCH_INSTALL_ASSET_DIR:-}" ]; then
    if [ -f "${UNCH_INSTALL_ASSET_DIR}/checksums.txt" ]; then
      printf '%s\n' "${UNCH_INSTALL_ASSET_DIR}/checksums.txt"
      return 0
    fi
    say "Missing checksums.txt in ${UNCH_INSTALL_ASSET_DIR}"
    return 1
  fi

  if ! has_cmd curl; then
    return 1
  fi

  checksums_path="${tmp_dir}/checksums.txt"
  url="https://github.com/${repo}/releases/download/${version}/checksums.txt"
  say "Downloading ${url}"
  curl -fsSL "$url" -o "${checksums_path}"
  printf '%s\n' "${checksums_path}"
}

verify_asset_checksum() {
  asset_path="$1"
  asset_name="$2"
  checksums_path="$3"

  expected_checksum="$(find_expected_checksum "$checksums_path" "$asset_name")"
  if [ -z "$expected_checksum" ]; then
    say "Could not find a SHA-256 checksum for ${asset_name} in ${checksums_path}"
    return 1
  fi

  actual_checksum="$(sha256_file "$asset_path" 2>/dev/null || true)"
  if [ -z "$actual_checksum" ]; then
    say "Could not verify ${asset_name}: no SHA-256 tool found (need sha256sum, shasum, or openssl)"
    return 1
  fi

  if [ "$actual_checksum" != "$expected_checksum" ]; then
    say "SHA-256 mismatch for ${asset_name}"
    say "Expected: ${expected_checksum}"
    say "Actual:   ${actual_checksum}"
    return 1
  fi
}

ensure_writable_dir() {
  target_dir="$1"
  if [ ! -d "$target_dir" ]; then
    mkdir -p "$target_dir" 2>/dev/null || return 1
  fi
  [ -w "$target_dir" ]
}

choose_default_bin_dir() {
  old_ifs="${IFS}"
  IFS=':'
  for candidate in $PATH; do
    [ -n "$candidate" ] || continue
    case "$candidate" in
      "${HOME}"/*|/opt/homebrew/bin|/usr/local/bin)
        if ensure_writable_dir "$candidate"; then
          IFS="${old_ifs}"
          printf '%s\n' "$candidate"
          return
        fi
        ;;
    esac
  done
  IFS="${old_ifs}"

  for candidate in "${HOME}/.local/bin" "${HOME}/bin" "/opt/homebrew/bin" "/usr/local/bin"; do
    if ensure_writable_dir "$candidate"; then
      printf '%s\n' "$candidate"
      return
    fi
  done

  printf '%s\n' "${HOME}/.local/bin"
}

detect_parent_shell() {
  shell_name="${SHELL:-sh}"
  shell_name="${shell_name##*/}"
  printf '%s\n' "$shell_name"
}

print_path_guidance() {
  target_dir="$1"
  binary_name="$2"

  say "To use unch now, run:"
  case "$(detect_parent_shell)" in
    fish)
      say "  set -gx PATH \"${target_dir}\" \$PATH"
      say "To keep it available in future fish sessions, add that line to ~/.config/fish/config.fish."
      ;;
    zsh)
      say "  export PATH=\"${target_dir}:\$PATH\""
      say "To keep it available in future zsh sessions, add that line to ~/.zshrc."
      ;;
    bash)
      say "  export PATH=\"${target_dir}:\$PATH\""
      say "To keep it available in future bash sessions, add that line to ~/.bashrc."
      ;;
    *)
      say "  export PATH=\"${target_dir}:\$PATH\""
      ;;
  esac
  say "Or run it directly:"
  say "  ${target_dir}/${binary_name}"
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

  BINARY_PATH="$binary_path" nix-shell -p patchelf stdenv.cc libffi pkg-config --run '
      set -eu
      linker="$(cat "$NIX_CC/nix-support/dynamic-linker")"
      glibc_dir="$(dirname "$linker")"
      libgcc_dir="$(dirname "$(cc -print-file-name=libgcc_s.so.1)")"
      libffi_dir="$(pkg-config --variable=libdir libffi)"
      patchelf --set-interpreter "$linker" --set-rpath "${glibc_dir}:${libgcc_dir}:${libffi_dir}" "$BINARY_PATH"
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
    cp "${UNCH_INSTALL_ASSET_DIR}/${asset}" "${asset_path}" || return 2
  else
    url="https://github.com/${repo}/releases/download/${version}/${asset}"
    say "Downloading ${url}"
    if ! curl -fsSL "$url" -o "${asset_path}"; then
      return 1
    fi
  fi

  checksums_path="$(resolve_checksums_file "$version" "$tmp_dir")" || return 2
  verify_asset_checksum "${asset_path}" "${asset}" "${checksums_path}" || return 2

  case "$archive_ext" in
    tar.gz)
      tar -xzf "${asset_path}" -C "${tmp_dir}" || return 2
      install_unix_binary "${tmp_dir}/${asset_binary}" "${bin_dir}/${asset_binary}" || return 2
      ;;
    zip)
      unzip -q "${asset_path}" -d "${tmp_dir}" || return 2
      install -m 0755 "${tmp_dir}/${asset_binary}" "${bin_dir}/${asset_binary}" || return 2
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

  say "Installing via go install ${source_pkg}${pkg_version}"
  GOBIN="${bin_dir}" go install "${source_pkg}${pkg_version}"
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

if [ -z "$bin_dir" ]; then
  bin_dir="$(choose_default_bin_dir)"
fi

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
  else
    archive_status=$?
    if [ "$archive_status" -eq 2 ]; then
      say "Release archive install failed verification or extraction; refusing to continue."
      exit 1
    fi
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
  *":${bin_dir}:"*)
    say "unch is now available on PATH."
    ;;
  *)
    say "Note: ${bin_dir} is not currently on PATH."
    print_path_guidance "${bin_dir}" "${installed_binary}"
    ;;
esac
