#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const https = require("node:https");
const os = require("node:os");
const path = require("node:path");
const { fileURLToPath } = require("node:url");
const { spawnSync } = require("node:child_process");
const { assetFor, releaseTagForVersion } = require("./platform");

const packageRoot = path.join(__dirname, "..");
const packageJSON = require(path.join(packageRoot, "package.json"));
const vendorDir = path.join(packageRoot, "vendor");

async function main() {
  if (process.env.UNCH_SKIP_DOWNLOAD === "1") {
    console.error("Skipping unch binary download because UNCH_SKIP_DOWNLOAD=1");
    return;
  }

  const asset = assetFor();
  const tag = process.env.UNCH_NPM_TAG || releaseTagForVersion(packageJSON.version);
  const url = process.env.UNCH_BINARY_URL || `https://github.com/uchebnick/unch/releases/download/${tag}/${asset.name}`;
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "unch-npm-"));
  const archivePath = path.join(tmpDir, asset.name);

  try {
    console.error(`Installing unch ${tag} for ${process.platform}/${process.arch}`);
    await fetchArchive(url, archivePath);

    const extractDir = path.join(tmpDir, "extract");
    fs.mkdirSync(extractDir, { recursive: true });
    extractArchive(asset.archive, archivePath, extractDir);

    const extractedBinary = findFile(extractDir, asset.binary);
    if (!extractedBinary) {
      throw new Error(`archive did not contain ${asset.binary}`);
    }

    fs.mkdirSync(vendorDir, { recursive: true });
    const targetPath = path.join(vendorDir, asset.binary);
    fs.copyFileSync(extractedBinary, targetPath);
    if (process.platform !== "win32") {
      fs.chmodSync(targetPath, 0o755);
    }
    console.error(`Installed ${asset.binary}`);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

async function fetchArchive(source, destination) {
  const localPath = localArchivePath(source);
  if (localPath) {
    fs.copyFileSync(localPath, destination);
    return;
  }
  await download(source, destination);
}

function localArchivePath(source) {
  const value = String(source || "").trim();
  if (value.startsWith("file://")) {
    return fileURLToPath(value);
  }
  if (/^https:\/\//i.test(value)) {
    return "";
  }
  return value;
}

function download(url, destination, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, (response) => {
      if ([301, 302, 303, 307, 308].includes(response.statusCode)) {
        response.resume();
        if (!response.headers.location || redirectsLeft <= 0) {
          reject(new Error(`download redirect failed for ${url}`));
          return;
        }
        const nextURL = new URL(response.headers.location, url).toString();
        download(nextURL, destination, redirectsLeft - 1).then(resolve, reject);
        return;
      }

      if (response.statusCode !== 200) {
        response.resume();
        reject(new Error(`download failed: ${response.statusCode} ${response.statusMessage}`));
        return;
      }

      const file = fs.createWriteStream(destination);
      response.pipe(file);
      file.on("finish", () => file.close(resolve));
      file.on("error", reject);
    });

    request.on("error", reject);
  });
}

function extractArchive(type, archivePath, extractDir) {
  if (type === "tar.gz") {
    run("tar", ["-xzf", archivePath, "-C", extractDir]);
    return;
  }

  if (type === "zip") {
    const tarResult = spawnSync("tar", ["-xf", archivePath, "-C", extractDir], {
      stdio: "ignore",
      windowsHide: true
    });
    if (tarResult.status === 0) {
      return;
    }

    if (process.platform === "win32") {
      run("powershell.exe", [
        "-NoProfile",
        "-NonInteractive",
        "-Command",
        `Expand-Archive -LiteralPath ${quotePowerShell(archivePath)} -DestinationPath ${quotePowerShell(extractDir)} -Force`
      ]);
      return;
    }

    run("unzip", ["-q", archivePath, "-d", extractDir]);
    return;
  }

  throw new Error(`unsupported archive type ${type}`);
}

function run(command, args) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    windowsHide: true
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(`${command} exited with status ${result.status}`);
  }
}

function findFile(root, name) {
  const entries = fs.readdirSync(root, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(root, entry.name);
    if (entry.isFile() && entry.name === name) {
      return fullPath;
    }
    if (entry.isDirectory()) {
      const found = findFile(fullPath, name);
      if (found) {
        return found;
      }
    }
  }
  return "";
}

function quotePowerShell(value) {
  return `'${String(value).replace(/'/g, "''")}'`;
}

if (require.main === module) {
  main().catch((error) => {
    console.error(`Failed to install unch: ${error.message}`);
    process.exit(1);
  });
}

module.exports = {
  download,
  extractArchive,
  findFile
};
