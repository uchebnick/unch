"use strict";

const platformAssets = {
  "darwin:arm64": {
    name: "unch_Darwin_arm64.tar.gz",
    binary: "unch",
    archive: "tar.gz"
  },
  "darwin:x64": {
    name: "unch_Darwin_x86_64.tar.gz",
    binary: "unch",
    archive: "tar.gz"
  },
  "linux:arm64": {
    name: "unch_Linux_arm64.tar.gz",
    binary: "unch",
    archive: "tar.gz"
  },
  "linux:x64": {
    name: "unch_Linux_x86_64.tar.gz",
    binary: "unch",
    archive: "tar.gz"
  },
  "win32:arm64": {
    name: "unch_Windows_arm64.zip",
    binary: "unch.exe",
    archive: "zip"
  },
  "win32:x64": {
    name: "unch_Windows_x86_64.zip",
    binary: "unch.exe",
    archive: "zip"
  }
};

function assetFor(platform = process.platform, arch = process.arch) {
  const key = `${platform}:${arch}`;
  const asset = platformAssets[key];
  if (!asset) {
    const supported = Object.keys(platformAssets).join(", ");
    throw new Error(`unsupported platform ${key}; supported: ${supported}`);
  }
  return asset;
}

function releaseTagForVersion(version) {
  const normalized = String(version || "").trim();
  if (!normalized) {
    throw new Error("package version is empty");
  }
  return normalized.startsWith("v") ? normalized : `v${normalized}`;
}

module.exports = {
  assetFor,
  releaseTagForVersion
};
