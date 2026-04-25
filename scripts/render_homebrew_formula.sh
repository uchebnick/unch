#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <version> <darwin_arm64_sha256> <darwin_x86_64_sha256>" >&2
  exit 1
fi

version="$1"
arm64_sha="$2"
amd64_sha="$3"

cat <<EOF
class Unch < Formula
  desc "Local-first semantic code search over code objects"
  homepage "https://github.com/uchebnick/unch"
  version "${version}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/uchebnick/unch/releases/download/v${version}/unch_Darwin_arm64.tar.gz"
      sha256 "${arm64_sha}"
    else
      url "https://github.com/uchebnick/unch/releases/download/v${version}/unch_Darwin_x86_64.tar.gz"
      sha256 "${amd64_sha}"
    end
  end

  def install
    bin.install "unch"
  end

  def caveats
    <<~EOS
      On first run unch may download:
        - a default GGUF embedding model into the user cache
        - yzma runtime libraries into the user cache
    EOS
  end

  test do
    assert_match "empty search query", shell_output("#{bin}/unch search 2>&1", 1)
  end
end
EOF
