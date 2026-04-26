#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const packageRoot = path.join(__dirname, "..");
const mcpLauncher = path.join(packageRoot, "bin", "unch-mcp.js");
const defaultStartupTimeoutSec = 60;
const defaultToolTimeoutSec = 300;

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
  const skillPath = path.join(codexHome, "skills", "unch", "SKILL.md");
  const legacyPromptPath = path.join(codexHome, "prompts", "unch.md");
  const mcpCommand = options.mcpCommand || process.execPath;
  const mcpArgs = options.mcpArgs.length > 0 ? options.mcpArgs : [mcpLauncher];

  if (options.dryRun) {
    process.stdout.write([
      "Would register Codex MCP server:",
      `  codex mcp add unch -- ${shellJoin([mcpCommand, ...mcpArgs])}`,
      `Would set startup_timeout_sec = ${defaultStartupTimeoutSec}`,
      `Would set tool_timeout_sec = ${defaultToolTimeoutSec}`,
      "Would install Codex skill:",
      `  ${skillPath}`,
      "Would remove legacy slash prompt if present:",
      `  ${legacyPromptPath}`,
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
    configureMcpTimeouts(codexHome, {
      startupTimeoutSec: defaultStartupTimeoutSec,
      toolTimeoutSec: defaultToolTimeoutSec
    });
  }

  if (!options.skipSkill) {
    installSkill(skillPath);
    removeLegacyPrompt(legacyPromptPath);
  }

  process.stdout.write([
    "Installed unch for Codex.",
    options.skipMcp ? "" : "MCP server: unch",
    options.skipSkill ? "" : "Skill: unch",
    "Restart Codex to load the new MCP server and skill.",
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
    skipSkill: false
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
      case "--skip-skill":
        options.skipSkill = true;
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

function installSkill(skillPath) {
  fs.mkdirSync(path.dirname(skillPath), { recursive: true });
  fs.writeFileSync(skillPath, skillText(), { mode: 0o644 });
}

function removeLegacyPrompt(promptPath) {
  if (fs.existsSync(promptPath)) {
    fs.rmSync(promptPath, { force: true });
  }
}

function configureMcpTimeouts(codexHome, { startupTimeoutSec, toolTimeoutSec }) {
  const configPath = path.join(codexHome, "config.toml");
  const text = fs.existsSync(configPath) ? fs.readFileSync(configPath, "utf8") : "";
  const lines = text.split(/\r?\n/);
  const header = "[mcp_servers.unch]";
  const start = lines.findIndex((line) => line.trim() === header);
  const timeoutLines = [
    `startup_timeout_sec = ${startupTimeoutSec}`,
    `tool_timeout_sec = ${toolTimeoutSec}`
  ];

  if (start === -1) {
    const prefix = text.trim() ? `${text.replace(/\s*$/, "")}\n\n` : "";
    fs.mkdirSync(codexHome, { recursive: true });
    fs.writeFileSync(configPath, `${prefix}${header}\n${timeoutLines.join("\n")}\n`);
    return;
  }

  let end = lines.length;
  for (let i = start + 1; i < lines.length; i += 1) {
    if (/^\s*\[/.test(lines[i])) {
      end = i;
      break;
    }
  }

  const block = lines.slice(start, end).filter((line) => {
    const trimmed = line.trim();
    return !trimmed.startsWith("startup_timeout_sec") && !trimmed.startsWith("tool_timeout_sec");
  });
  const nextLines = [
    ...lines.slice(0, start),
    ...block,
    ...timeoutLines,
    ...lines.slice(end)
  ];
  fs.writeFileSync(configPath, `${nextLines.join("\n").replace(/\s*$/, "")}\n`);
}

function skillText() {
  return [
    "---",
    "name: unch",
    "description: Use when working in a code repository and the user asks to find, understand, debug, review, or modify code. Prefer unch semantic code search before broad file reads, especially for concepts, APIs, implementations, identifiers, error paths, or architecture questions.",
    "---",
    "",
    "# unch",
    "",
    "Use unch semantic code search for the current repository before broad file reads.",
    "",
    "Always pass `directory` as the absolute path of the current repository/workspace in every unch MCP tool call. Do not rely on the MCP server launch directory when the workspace path is known.",
    "",
    "Workflow:",
    "1. Call the unch MCP tool `workspace_status` with `directory`.",
    "2. If `workspace_status` shows `remote_ci_url`, call `remote_sync_index` with the same `directory` before rebuilding locally.",
    "3. If the index is missing for the current provider/model, call `index_repository` once with the same `directory`.",
    "4. Call `search_code` with the same `directory` and my task, bug, feature, identifier, or concept.",
    "5. Use `details=true` when signatures, comments, docs, or compact body snippets help choose exact files.",
    "6. Treat results as ranked candidates and open returned paths before editing.",
    "",
    "Remote/CI workflow:",
    "- Use `create_ci_workflow` only when I ask to set up GitHub Actions-backed indexing.",
    "- Use `bind_remote_ci` after the workflow exists or when I provide a GitHub repository/workflow URL to bind.",
    "- Use `remote_download_index` when I ask to fetch an index for a specific commit.",
    "",
    "If the unch MCP tools are unavailable, tell me to run `unch codex install` and restart Codex."
  ].join("\n");
}

function helpText() {
  return [
    "Usage: unch codex install [options]",
    "",
    "Registers unch with Codex as an MCP server and installs the unch skill.",
    "",
    "Options:",
    "  --codex-bin <path>     Codex executable to call (default: codex)",
    "  --codex-home <path>    Codex home directory (default: $CODEX_HOME or ~/.codex)",
    "  --dry-run              Print planned changes without writing anything",
    "  --mcp-command <path>   MCP command to register (default: current node)",
    "  --mcp-arg <value>      MCP command argument; repeatable",
    "  --skip-mcp             Do not register the MCP server",
    "  --skip-skill           Do not install the unch skill",
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
  configureMcpTimeouts,
  main,
  parseArgs,
  skillText
};
