#!/usr/bin/env node

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const PLATFORM_MAP = {
  "linux-x64": "lark-daemon-linux-amd64",
  "linux-arm64": "lark-daemon-linux-arm64",
  "darwin-x64": "lark-daemon-darwin-amd64",
  "darwin-arm64": "lark-daemon-darwin-arm64",
  "win32-x64": "lark-daemon-windows-amd64.exe",
};

function getBinaryName() {
  const key = `${process.platform}-${process.arch}`;
  const name = PLATFORM_MAP[key];
  if (!name) {
    console.error(`Unsupported platform: ${key}`);
    console.error(`Supported: ${Object.keys(PLATFORM_MAP).join(", ")}`);
    process.exit(1);
  }
  return name;
}

function getBinaryPath() {
  const name = getBinaryName();
  return path.join(__dirname, name);
}

const binaryPath = getBinaryPath();

if (!fs.existsSync(binaryPath)) {
  console.error("lark-daemon binary not found.");
  console.error("Run `npm install` in this package directory to download it,");
  console.error("or download manually from:");
  console.error("  https://github.com/GrayCodeAI/lark-daemon/releases");
  process.exit(1);
}

const args = process.argv.slice(2);
const child = spawn(binaryPath, args, { stdio: "inherit" });

child.on("exit", (code) => {
  process.exit(code ?? 0);
});

child.on("error", (err) => {
  if (err.code === "EACCES") {
    console.error(`Permission denied: ${binaryPath}`);
    console.error(`Run: chmod +x ${binaryPath}`);
  } else {
    console.error(err);
  }
  process.exit(1);
});
