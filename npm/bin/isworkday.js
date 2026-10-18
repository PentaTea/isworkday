#!/usr/bin/env node
// Runs the prebuilt binary for this platform; exits 127 (a shell's "command not found") without one.
const { spawnSync } = require("node:child_process");
const path = require("node:path");

const os = { darwin: "Darwin", linux: "Linux" }[process.platform];
const arch = { x64: "x86_64", arm64: process.platform === "linux" ? "aarch64" : "arm64" }[process.arch];
if (!os || !arch) {
  console.error(`isworkday: unsupported platform ${process.platform}/${process.arch}`);
  process.exit(127);
}

const bin = path.join(__dirname, "..", "vendor", `isworkday-${os}-${arch}`);
const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  console.error(`isworkday: cannot run ${bin}: ${r.error.message}`);
  process.exit(127);
}
if (r.signal) process.kill(process.pid, r.signal); // die by the same signal
process.exit(r.status ?? 1);
