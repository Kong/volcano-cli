'use strict';

// Shared helpers for downloading and verifying the platform-specific Volcano
// CLI binary from GitHub Releases. Used by the postinstall script
// (scripts/npm/install.js) and, as a self-healing fallback, by the launcher
// shim (bin/volcano.js).

const fs = require('fs');
const path = require('path');
const http = require('http');
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

// Which JS package manager is driving this install. During a lifecycle script
// npm/pnpm/yarn/bun all set npm_config_user_agent (e.g. "pnpm/8.6.0 ...").
function detectManager(ua = process.env.npm_config_user_agent || '') {
  const name = String(ua).split('/')[0].trim().toLowerCase();
  return ['npm', 'pnpm', 'yarn', 'bun'].includes(name) ? name : 'npm';
}

// Record how the CLI was installed next to the binary so `volcano upgrade`
// delegates to the right package manager. Best effort: a missing marker just
// falls back to path-based detection in the Go binary.
function writeInstallMarker() {
  const ua = process.env.npm_config_user_agent;
  // Only a package-manager lifecycle script sets a user agent. At shim runtime
  // (self-heal after --ignore-scripts / VOLCANO_SKIP_DOWNLOAD) there is none, so
  // skip the marker rather than guess `npm` and mislabel a pnpm/yarn/bun install
  // — the Go binary's path-based detection is accurate in that case.
  if (!ua) return;
  const marker = path.join(path.dirname(binaryPath()), '.volcano-install-method');
  try {
    fs.mkdirSync(path.dirname(marker), { recursive: true });
    fs.writeFileSync(marker, `${detectManager(ua)}\n`);
  } catch {
    // best effort
  }
}

function releaseTag() {
  // Staging prereleases (`X.Y.Z-staging[.N]`, published under the npm `staging`
  // dist-tag) resolve to the immutable `staging-vX.Y.Z` GitHub release. Every
  // other version maps 1:1 to the `v<version>` release tag.
  const staging = /^(\d+\.\d+\.\d+)-staging(?:\.\d+)?$/.exec(pkg.version);
  if (staging) {
    return `staging-v${staging[1]}`;
  }
  return `v${pkg.version}`;
}

function get(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 10) {
      reject(new Error(`Too many redirects while fetching ${url}`));
      return;
    }
    let protocol;
    try {
      protocol = new URL(url).protocol;
    } catch (err) {
      reject(new Error(`Invalid download URL "${url}": ${err.message}`));
      return;
    }
    // Pick the client by scheme so an http:// mirror
    // (VOLCANO_GITHUB_RELEASES_URL) works too.
    const client = protocol === 'https:' ? https : protocol === 'http:' ? http : null;
    if (!client) {
      reject(new Error(`Unsupported URL scheme "${protocol}" for ${url}; use http or https.`));
      return;
    }
    const req = client.get(
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
  const tmp = `${dest}.download-${process.pid}`;
  try {
    const res = await get(url);
    await streamPipeline(res, fs.createWriteStream(tmp));
    // Hash the fully-written file so we verify exactly what landed on disk
    // (also avoids the flowing-mode subtleties of hashing mid-stream).
    const hash = crypto.createHash('sha256');
    await streamPipeline(fs.createReadStream(tmp), hash);
    const digest = hash.digest('hex');
    // rename does not overwrite an existing destination on Windows, so clear it
    // first (also covers a check-then-write race and force re-downloads).
    fs.rmSync(dest, { force: true });
    fs.renameSync(tmp, dest);
    return digest;
  } catch (err) {
    // Never leave a partial download behind.
    fs.rmSync(tmp, { force: true });
    throw err;
  }
}

// Download the correct binary for the current platform and verify its checksum
// against the release's SHA256SUMS manifest. Idempotent: no-op if present.
async function ensureBinary({ force = false } = {}) {
  const dest = binaryPath();
  writeInstallMarker();
  if (!force && fs.existsSync(dest)) {
    return dest;
  }

  if (pkg.version === '0.0.0') {
    throw new Error(
      'No published binary for development version 0.0.0; build from source ' +
        'with `make build` or install a released version.'
    );
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
