#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const packageRoot = path.join(__dirname, "..");
const mcpLauncher = path.join(packageRoot, "bin", "unch-mcp.js");

async function main(argv = process.argv.slice(2), env = process.env) {
  const options = parseArgs(argv);
  if (options.help) {
    process.stdout.write(helpText());
    return;
  }

  if (options.command !== "install") {
    throw new Error(`unknown unch Codex command: ${options.command}`);
  }

  const codexHome = path.resolve(options.codexHome || env.CODEX_HOME || path.join(os.homedir(), ".codex"));
  const promptPath = path.join(codexHome, "prompts", "unch.md");
  const mcpCommand = options.mcpCommand || process.execPath;
  const mcpArgs = options.mcpArgs.length > 0 ? options.mcpArgs : [mcpLauncher];

  if (options.dryRun) {
    process.stdout.write([
      "Would register Codex MCP server:",
      `  codex mcp add unch -- ${shellJoin([mcpCommand, ...mcpArgs])}`,
      "Would install slash prompt:",
      `  ${promptPath}`,
      ""
    ].join("\n"));
    return;
  }

  if (!options.skipMcp) {
    registerMcp({
      codexBin: options.codexBin || env.CODEX_BIN || "codex",
      codexHome,
      command: mcpCommand,
      args: mcpArgs
    });
  }

  if (!options.skipPrompt) {
    installPrompt(promptPath);
  }

  process.stdout.write([
    "Installed unch for Codex.",
    options.skipMcp ? "" : "MCP server: unch",
    options.skipPrompt ? "" : "Slash prompt: /unch",
    "Restart Codex to load the new MCP server and slash prompt.",
    ""
  ].filter(Boolean).join("\n"));
}

function parseArgs(argv) {
  const options = {
    command: "install",
    codexBin: "",
    codexHome: "",
    dryRun: false,
    help: false,
    mcpArgs: [],
    mcpCommand: "",
    skipMcp: false,
    skipPrompt: false
  };

  const args = [...argv];
  if (args[0] && !args[0].startsWith("-")) {
    options.command = args.shift();
  }

  while (args.length > 0) {
    const arg = args.shift();
    switch (arg) {
      case "-h":
      case "--help":
        options.help = true;
        break;
      case "--codex-bin":
        options.codexBin = takeValue(arg, args);
        break;
      case "--codex-home":
        options.codexHome = takeValue(arg, args);
        break;
      case "--dry-run":
        options.dryRun = true;
        break;
      case "--mcp-command":
        options.mcpCommand = takeValue(arg, args);
        break;
      case "--mcp-arg":
        options.mcpArgs.push(takeValue(arg, args));
        break;
      case "--skip-mcp":
        options.skipMcp = true;
        break;
      case "--skip-prompt":
        options.skipPrompt = true;
        break;
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }

  return options;
}

function takeValue(option, args) {
  const value = args.shift();
  if (!value) {
    throw new Error(`${option} requires a value`);
  }
  return value;
}

function registerMcp({ codexBin, codexHome, command, args }) {
  const result = spawnSync(codexBin, ["mcp", "add", "unch", "--", command, ...args], {
    env: { ...process.env, CODEX_HOME: codexHome },
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true
  });

  if (result.error) {
    throw new Error(`failed to run ${codexBin}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    const details = (result.stderr || result.stdout || "").trim();
    throw new Error(`failed to register unch MCP with Codex${details ? `: ${details}` : ""}`);
  }
}

function installPrompt(promptPath) {
  fs.mkdirSync(path.dirname(promptPath), { recursive: true });
  fs.writeFileSync(promptPath, promptText(), { mode: 0o644 });
}

function promptText() {
  return [
    "Use unch semantic code search for this repository before broad file reads.",
    "",
    "Workflow:",
    "1. Call the unch MCP tool `workspace_status`.",
    "2. If the index is missing for the current provider/model, call `index_repository` once.",
    "3. Call `search_code` with my task, bug, feature, identifier, or concept.",
    "4. Use `details=true` when signatures, comments, docs, or compact body snippets help choose exact files.",
    "5. Treat results as ranked candidates and open returned paths before editing.",
    "",
    "If the unch MCP tools are unavailable, tell me to run `unch codex install` and restart Codex."
  ].join("\n");
}

function helpText() {
  return [
    "Usage: unch codex install [options]",
    "",
    "Registers unch with Codex as an MCP server and installs the /unch slash prompt.",
    "",
    "Options:",
    "  --codex-bin <path>     Codex executable to call (default: codex)",
    "  --codex-home <path>    Codex home directory (default: $CODEX_HOME or ~/.codex)",
    "  --dry-run              Print planned changes without writing anything",
    "  --mcp-command <path>   MCP command to register (default: current node)",
    "  --mcp-arg <value>      MCP command argument; repeatable",
    "  --skip-mcp             Do not register the MCP server",
    "  --skip-prompt          Do not install the /unch prompt",
    "  -h, --help             Show this help",
    ""
  ].join("\n");
}

function shellJoin(parts) {
  return parts.map((part) => {
    if (/^[A-Za-z0-9_./:=+-]+$/.test(part)) {
      return part;
    }
    return `'${String(part).replace(/'/g, "'\\''")}'`;
  }).join(" ");
}

if (require.main === module) {
  main().catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
}

module.exports = {
  main,
  parseArgs,
  promptText
};
