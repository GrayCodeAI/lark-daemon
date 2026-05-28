#!/usr/bin/env node

const https = require("https");
const http = require("http");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const VERSION = require("./package.json").version;
const REPO = "GrayCodeAI/lark-daemon";

const PLATFORM_MAP = {
  "linux-x64": "lark-daemon-linux-amd64",
  "linux-arm64": "lark-daemon-linux-arm64",
  "darwin-x64": "lark-daemon-darwin-amd64",
  "darwin-arm64": "lark-daemon-darwin-arm64",
  "win32-x64": "lark-daemon-windows-amd64.exe",
};

function getPlatformKey() {
  return `${process.platform}-${process.arch}`;
}

function getDownloadURL() {
  const key = getPlatformKey();
  const binaryName = PLATFORM_MAP[key];
  if (!binaryName) {
    console.warn(`No prebuilt binary for ${key}. Build from source:`);
    console.warn(`  git clone https://github.com/${REPO}.git`);
    console.warn(`  cd lark-daemon && make build`);
    process.exit(0);
  }
  const tag = `v${VERSION}`;
  return `https://github.com/${REPO}/releases/download/${tag}/${binaryName}`;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith("https") ? https : http;
    const followRedirects = (url, redirects = 0) => {
      if (redirects > 5) return reject(new Error("too many redirects"));
      client
        .get(url, (res) => {
          if (
            res.statusCode >= 300 &&
            res.statusCode < 400 &&
            res.headers.location
          ) {
            return followRedirects(res.headers.location, redirects + 1);
          }
          if (res.statusCode !== 200) {
            return reject(
              new Error(`download failed: HTTP ${res.statusCode} for ${url}`)
            );
          }
          const file = fs.createWriteStream(dest);
          res.pipe(file);
          file.on("finish", () => file.close(resolve));
          file.on("error", reject);
        })
        .on("error", reject);
    };
    followRedirects(url);
  });
}

async function main() {
  const key = getPlatformKey();
  const binaryName = PLATFORM_MAP[key];
  if (!binaryName) {
    console.warn(`No prebuilt binary for ${key}. Skipping download.`);
    console.warn("Build from source: https://github.com/" + REPO);
    return;
  }

  const dest = path.join(__dirname, "bin", binaryName);
  if (fs.existsSync(dest)) {
    console.log(`lark-daemon binary already present: ${binaryName}`);
    return;
  }

  const url = getDownloadURL();
  console.log(`Downloading lark-daemon ${VERSION} for ${key}...`);
  console.log(`  ${url}`);

  try {
    await download(url, dest);
    fs.chmodSync(dest, 0o755);
    console.log(`Installed: ${binaryName}`);
  } catch (err) {
    console.warn(`Download failed: ${err.message}`);
    console.warn("Build from source: https://github.com/" + REPO);
    // Don't fail install — user can build manually
  }
}

main();
