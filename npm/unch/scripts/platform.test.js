"use strict";

const assert = require("node:assert/strict");
const { assetFor, releaseTagForVersion } = require("./platform");

assert.equal(assetFor("darwin", "arm64").name, "unch_Darwin_arm64.tar.gz");
assert.equal(assetFor("darwin", "x64").name, "unch_Darwin_x86_64.tar.gz");
assert.equal(assetFor("linux", "arm64").name, "unch_Linux_arm64.tar.gz");
assert.equal(assetFor("linux", "x64").name, "unch_Linux_x86_64.tar.gz");
assert.equal(assetFor("win32", "arm64").name, "unch_Windows_arm64.zip");
assert.equal(assetFor("win32", "x64").name, "unch_Windows_x86_64.zip");

assert.equal(releaseTagForVersion("0.3.11"), "v0.3.11");
assert.equal(releaseTagForVersion("v0.3.11"), "v0.3.11");

assert.throws(() => assetFor("freebsd", "x64"), /unsupported platform/);
assert.throws(() => releaseTagForVersion(""), /package version is empty/);

console.log("platform mapping ok");
