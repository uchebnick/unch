"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { promptText } = require("./codex-install");

const root = path.join(__dirname, "..");
const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "unch-codex-install-test-"));

try {
  const codexHome = path.join(tempDir, "codex-home");
  const logPath = path.join(tempDir, "codex-args.jsonl");
  const fakeCodex = path.join(tempDir, process.platform === "win32" ? "codex.cmd" : "codex");
  writeFakeCodex(fakeCodex, logPath);

  const result = spawnSync(process.execPath, ["bin/unch.js", "codex", "install"], {
    cwd: root,
    env: {
      ...process.env,
      CODEX_BIN: fakeCodex,
      CODEX_HOME: codexHome
    },
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Installed unch for Codex/);

  const promptPath = path.join(codexHome, "prompts", "unch.md");
  assert.equal(fs.readFileSync(promptPath, "utf8"), promptText());

  const configText = fs.readFileSync(path.join(codexHome, "config.toml"), "utf8");
  assert.match(configText, /\[mcp_servers\.unch\]/);
  assert.match(configText, /startup_timeout_sec = 60/);
  assert.match(configText, /tool_timeout_sec = 300/);

  const calls = fs.readFileSync(logPath, "utf8").trim().split("\n").map(JSON.parse);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0].slice(0, 5), ["mcp", "add", "unch", "--", process.execPath]);
  assert.equal(calls[0][5], path.join(root, "bin", "unch-mcp.js"));

  const dryRunDir = path.join(tempDir, "dry-run-home");
  const dryRun = spawnSync(process.execPath, ["bin/unch.js", "codex", "install", "--dry-run", "--codex-home", dryRunDir], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true
  });
  assert.equal(dryRun.status, 0, dryRun.stderr);
  assert.match(dryRun.stdout, /Would register Codex MCP server/);
  assert.equal(fs.existsSync(dryRunDir), false);

  console.log("codex install ok");
} finally {
  fs.rmSync(tempDir, { recursive: true, force: true });
}

function writeFakeCodex(file, logPath) {
  if (process.platform === "win32") {
    fs.writeFileSync(file, `@echo off\r\nnode -e "require('fs').appendFileSync(process.argv[1], JSON.stringify(process.argv.slice(2)) + '\\n')" "${logPath}" %*\r\n`);
    return;
  }

  fs.writeFileSync(file, [
    "#!/usr/bin/env node",
    `"use strict";`,
    `const fs = require("node:fs");`,
    `const path = require("node:path");`,
    `fs.appendFileSync(${JSON.stringify(logPath)}, JSON.stringify(process.argv.slice(2)) + "\\n");`,
    `const codexHome = process.env.CODEX_HOME;`,
    `if (codexHome) {`,
    `  fs.mkdirSync(codexHome, { recursive: true });`,
    `  fs.writeFileSync(path.join(codexHome, "config.toml"), "[mcp_servers.unch]\\ncommand = \\"node\\"\\nargs = [\\"bin/unch-mcp.js\\"]\\n");`,
    `}`
  ].join("\n"));
  fs.chmodSync(file, 0o755);
}
