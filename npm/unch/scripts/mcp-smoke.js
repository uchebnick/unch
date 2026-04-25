"use strict";

const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");

const command = process.env.UNCH_MCP_COMMAND || "unch-mcp";

function frame(message) {
  const body = Buffer.from(JSON.stringify(message));
  return Buffer.concat([
    Buffer.from(`Content-Length: ${body.length}\r\n\r\n`),
    body
  ]);
}

function readFrames(buffer) {
  const messages = [];
  let offset = 0;
  while (offset < buffer.length) {
    const headerEnd = buffer.indexOf("\r\n\r\n", offset);
    assert.notEqual(headerEnd, -1, "missing MCP header terminator");

    const header = buffer.slice(offset, headerEnd).toString("utf8");
    const match = /^Content-Length:\s*(\d+)$/im.exec(header);
    assert.ok(match, `missing Content-Length header: ${header}`);

    const length = Number(match[1]);
    const bodyStart = headerEnd + 4;
    const bodyEnd = bodyStart + length;
    assert.ok(bodyEnd <= buffer.length, "truncated MCP frame");

    messages.push(JSON.parse(buffer.slice(bodyStart, bodyEnd).toString("utf8")));
    offset = bodyEnd;
  }
  return messages;
}

async function main() {
  const child = spawn(command, [], {
    stdio: ["pipe", "pipe", "pipe"],
    shell: process.platform === "win32",
    windowsHide: true
  });

  const stdout = [];
  const stderr = [];
  child.stdout.on("data", (chunk) => stdout.push(chunk));
  child.stderr.on("data", (chunk) => stderr.push(chunk));

  child.stdin.write(frame({
    jsonrpc: "2.0",
    id: 1,
    method: "initialize",
    params: { protocolVersion: "2025-11-25" }
  }));
  child.stdin.write(frame({
    jsonrpc: "2.0",
    id: 2,
    method: "prompts/list"
  }));
  child.stdin.write(frame({
    jsonrpc: "2.0",
    id: 3,
    method: "prompts/get",
    params: {
      name: "unch",
      arguments: { query: "router middleware" }
    }
  }));
  child.stdin.end();

  const exitCode = await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("exit", resolve);
  });

  const stderrText = Buffer.concat(stderr).toString("utf8");
  assert.equal(exitCode, 0, stderrText);
  assert.equal(stderrText, "");

  const messages = readFrames(Buffer.concat(stdout));
  assert.equal(messages.length, 3);
  assert.ok(messages[0].result.capabilities.prompts);
  assert.equal(messages[1].result.prompts[0].name, "unch");

  const promptText = messages[2].result.messages[0].content.text;
  assert.match(promptText, /workspace_status/);
  assert.match(promptText, /search_code/);
  assert.match(promptText, /router middleware/);

  console.log("mcp smoke ok");
}

main().catch((error) => {
  console.error(error.stack || error.message);
  process.exit(1);
});
