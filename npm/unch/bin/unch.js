#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const binaryName = process.platform === "win32" ? "unch.exe" : "unch";
const binaryPath = path.join(__dirname, "..", "vendor", binaryName);

if (!fs.existsSync(binaryPath)) {
  console.error("unch native binary is missing.");
  console.error("Try reinstalling the package: npm install -g @uchebnick/unch");
  process.exit(1);
}

const result = spawnSync(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  windowsHide: false
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}

process.exit(result.status ?? 0);
