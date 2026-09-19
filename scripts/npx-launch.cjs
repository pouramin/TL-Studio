#!/usr/bin/env node
"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const crypto = require("crypto");
const { spawnSync } = require("child_process");

const REPO = "pouramin/TL-Studio";
const USER_AGENT = "TL-Studio-npx-launcher";
const args = process.argv.slice(2);

function fail(message) {
  console.error(`TL Studio quick launch failed: ${message}`);
  process.exit(1);
}

function request(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 8) return reject(new Error("too many redirects"));
    const req = https.get(url, {
      headers: {
        "User-Agent": USER_AGENT,
        "Accept": "application/vnd.github+json",
      },
    }, (res) => {
      const status = res.statusCode || 0;
      if (status >= 300 && status < 400 && res.headers.location) {
        res.resume();
        return resolve(request(new URL(res.headers.location, url).toString(), redirects + 1));
      }
      if (status < 200 || status >= 300) {
        let body = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => { body += chunk; });
        res.on("end", () => reject(new Error(`HTTP ${status}: ${body.slice(0, 240)}`)));
        return;
      }
      resolve(res);
    });
    req.on("error", reject);
  });
}

async function fetchJSON(url) {
  const res = await request(url);
  let body = "";
  res.setEncoding("utf8");
  for await (const chunk of res) body += chunk;
  return JSON.parse(body);
}

async function download(url, destination) {
  const res = await request(url);
  await fs.promises.mkdir(path.dirname(destination), { recursive: true });
  const out = fs.createWriteStream(destination);
  await new Promise((resolve, reject) => {
    res.pipe(out);
    out.on("finish", () => out.close(resolve));
    out.on("error", reject);
    res.on("error", reject);
  });
}

async function sha256(file) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const input = fs.createReadStream(file);
    input.on("data", (chunk) => hash.update(chunk));
    input.on("end", () => resolve(hash.digest("hex")));
    input.on("error", reject);
  });
}

function platformTarget() {
  const arch = process.arch;
  if (process.platform === "win32" && arch === "x64") {
    return { suffix: "windows-x64.zip", binary: "tl-studio.exe", archive: "zip" };
  }
  if (process.platform === "linux" && arch === "x64") {
    return { suffix: "linux-x64.tar.gz", binary: "tl-studio", archive: "tar" };
  }
  if (process.platform === "linux" && arch === "arm64") {
    return { suffix: "linux-arm64.tar.gz", binary: "tl-studio", archive: "tar" };
  }
  if (process.platform === "darwin" && arch === "x64") {
    return { suffix: "macos-x64.tar.gz", binary: "tl-studio", archive: "tar" };
  }
  if (process.platform === "darwin" && arch === "arm64") {
    return { suffix: "macos-arm64.tar.gz", binary: "tl-studio", archive: "tar" };
  }
  fail(`unsupported platform: ${process.platform}/${arch}`);
}

function cacheRoot() {
  if (process.platform === "win32") {
    return path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local"), "TL-Studio", "cache");
  }
  return path.join(process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"), "tl-studio");
}

function psQuote(value) {
  return `'${String(value).replace(/'/g, "''")}'`;
}

function extract(archive, destination, kind) {
  fs.mkdirSync(destination, { recursive: true });
  if (kind === "zip") {
    const command = `Expand-Archive -LiteralPath ${psQuote(archive)} -DestinationPath ${psQuote(destination)} -Force`;
    const result = spawnSync("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", command], { stdio: "inherit" });
    if (result.status !== 0) fail("failed to extract the Windows release archive");
    return;
  }
  const result = spawnSync("tar", ["-xzf", archive, "-C", destination], { stdio: "inherit" });
  if (result.status !== 0) fail("failed to extract the release archive");
}

async function main() {
  const target = platformTarget();
  const requestedTag = process.env.TL_STUDIO_VERSION || "";
  const releases = await fetchJSON(`https://api.github.com/repos/${REPO}/releases?per_page=30`);
  const release = releases.find((item) => {
    if (item.draft) return false;
    if (requestedTag && item.tag_name !== requestedTag) return false;
    return Array.isArray(item.assets) && item.assets.some((asset) => asset.name.endsWith(target.suffix));
  });
  if (!release) fail(requestedTag ? `release ${requestedTag} is unavailable for this platform` : "no compatible GitHub release was found");

  const asset = release.assets.find((item) => item.name.endsWith(target.suffix));
  const sums = release.assets.find((item) => item.name === "SHA256SUMS.txt");
  if (!asset || !sums) fail("release assets or SHA256SUMS.txt are missing");

  const versionDir = path.join(cacheRoot(), release.tag_name);
  const binary = path.join(versionDir, target.binary);
  if (!fs.existsSync(binary)) {
    const tempDir = path.join(cacheRoot(), `.download-${process.pid}-${Date.now()}`);
    const archivePath = path.join(tempDir, asset.name);
    const sumsPath = path.join(tempDir, "SHA256SUMS.txt");
    try {
      fs.mkdirSync(tempDir, { recursive: true });
      console.log(`Downloading TL Studio ${release.tag_name} for ${process.platform}/${process.arch}…`);
      await Promise.all([
        download(asset.browser_download_url, archivePath),
        download(sums.browser_download_url, sumsPath),
      ]);

      const lines = fs.readFileSync(sumsPath, "utf8").split(/\r?\n/);
      const checksumLine = lines.find((line) => line.trim().endsWith(asset.name));
      if (!checksumLine) fail(`checksum for ${asset.name} was not found`);
      const expected = checksumLine.trim().split(/\s+/)[0].toLowerCase();
      const actual = (await sha256(archivePath)).toLowerCase();
      if (actual !== expected) fail(`SHA-256 mismatch for ${asset.name}`);

      fs.rmSync(versionDir, { recursive: true, force: true });
      extract(archivePath, versionDir, target.archive);
      if (!fs.existsSync(binary)) fail(`release did not contain ${target.binary}`);
      if (process.platform !== "win32") fs.chmodSync(binary, 0o755);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }

  console.log(`Launching TL Studio ${release.tag_name} in ${process.cwd()}`);
  const result = spawnSync(binary, args, {
    cwd: process.cwd(),
    stdio: "inherit",
    env: process.env,
  });
  if (result.error) fail(result.error.message);
  process.exit(typeof result.status === "number" ? result.status : 0);
}

main().catch((error) => fail(error && error.message ? error.message : String(error)));
