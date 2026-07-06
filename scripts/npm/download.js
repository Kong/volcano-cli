'use strict';

// Shared helpers for downloading and verifying the platform-specific Volcano
// CLI binary from GitHub Releases. Used by the postinstall script
// (scripts/npm/install.js) and, as a self-healing fallback, by the launcher
// shim (bin/volcano.js).

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { pipeline } = require('stream');
const { promisify } = require('util');

const streamPipeline = promisify(pipeline);

const pkg = require('../../package.json');

// Allow pointing at a mirror (kept consistent with scripts/install-volcano.sh).
const RELEASES_BASE = (
  process.env.VOLCANO_GITHUB_RELEASES_URL ||
  'https://github.com/Kong/volcano-cli/releases'
).replace(/\/+$/, '');

// Map Node's platform-arch to the release asset target id.
const TARGETS = {
  'linux-x64': 'linux-amd64',
  'linux-arm64': 'linux-arm64',
  'darwin-x64': 'macos-amd64',
  'darwin-arm64': 'macos-arm64',
  'win32-x64': 'windows-amd64',
};

function resolveTarget() {
  const key = `${process.platform}-${process.arch}`;
  const target = TARGETS[key];
  if (!target) {
    throw new Error(
      `Unsupported platform "${key}". Volcano CLI ships binaries for: ` +
        `${Object.keys(TARGETS).join(', ')}.`
    );
  }
  return target;
}

function binaryExt() {
  return process.platform === 'win32' ? '.exe' : '';
}

function assetName() {
  return `volcano-${resolveTarget()}${binaryExt()}`;
}

// Absolute path where the downloaded binary lives (next to the launcher shim).
function binaryPath() {
  return path.join(__dirname, '..', '..', 'bin', assetName());
}

function releaseTag() {
  // The npm package version maps 1:1 to the GitHub release tag `v<version>`.
  return `v${pkg.version}`;
}

function get(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 10) {
      reject(new Error(`Too many redirects while fetching ${url}`));
      return;
    }
    const req = https.get(
      url,
      { headers: { 'User-Agent': `volcano-cli-npm/${pkg.version}` } },
      (res) => {
        const { statusCode, headers } = res;
        if (statusCode >= 300 && statusCode < 400 && headers.location) {
          res.resume();
          const next = new URL(headers.location, url).toString();
          resolve(get(next, redirects + 1));
          return;
        }
        if (statusCode !== 200) {
          res.resume();
          reject(new Error(`Request to ${url} failed with status ${statusCode}`));
          return;
        }
        resolve(res);
      }
    );
    req.on('error', reject);
    req.setTimeout(60000, () => {
      req.destroy(new Error(`Request to ${url} timed out`));
    });
  });
}

async function getText(url) {
  const res = await get(url);
  const chunks = [];
  for await (const chunk of res) chunks.push(chunk);
  return Buffer.concat(chunks).toString('utf8');
}

// Parse a `shasum -a 256` style manifest and return the hex digest for `name`.
function parseChecksum(manifest, name) {
  const lines = manifest.split('\n');
  for (const line of lines) {
    const match = line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (match && match[2].trim() === name) {
      return match[1].toLowerCase();
    }
  }
  return null;
}

async function downloadToFile(url, dest) {
  const hash = crypto.createHash('sha256');
  const tmp = `${dest}.download-${process.pid}`;
  const res = await get(url);
  res.on('data', (chunk) => hash.update(chunk));
  await streamPipeline(res, fs.createWriteStream(tmp));
  fs.renameSync(tmp, dest);
  return hash.digest('hex');
}

// Download the correct binary for the current platform and verify its checksum
// against the release's SHA256SUMS manifest. Idempotent: no-op if present.
async function ensureBinary({ force = false } = {}) {
  const dest = binaryPath();
  if (!force && fs.existsSync(dest)) {
    return dest;
  }

  const name = assetName();
  const tag = releaseTag();
  const base = `${RELEASES_BASE}/download/${tag}`;

  const manifest = await getText(`${base}/SHA256SUMS`);
  const expected = parseChecksum(manifest, name);
  if (!expected) {
    throw new Error(`No SHA256 checksum for ${name} in ${tag}/SHA256SUMS`);
  }

  fs.mkdirSync(path.dirname(dest), { recursive: true });
  const actual = (await downloadToFile(`${base}/${name}`, dest)).toLowerCase();
  if (actual !== expected) {
    fs.rmSync(dest, { force: true });
    throw new Error(
      `Checksum mismatch for ${name}: expected ${expected}, got ${actual}`
    );
  }

  if (process.platform !== 'win32') {
    fs.chmodSync(dest, 0o755);
  }
  return dest;
}

module.exports = {
  resolveTarget,
  assetName,
  binaryPath,
  releaseTag,
  ensureBinary,
};
