#!/usr/bin/env node
'use strict';

// Launcher shim: exec the platform-specific Volcano CLI binary, forwarding all
// arguments, stdio, and the exit code. If the binary is missing (e.g. install
// ran with --ignore-scripts, or the postinstall download failed) it is fetched
// and verified on demand.

const fs = require('fs');
const { spawn } = require('child_process');
const { binaryPath, ensureBinary } = require('../scripts/npm/download.js');

async function resolveBinary() {
  try {
    const bin = binaryPath();
    if (fs.existsSync(bin)) {
      return bin;
    }
    return await ensureBinary();
  } catch (err) {
    // Covers both download failures and unsupported platforms: binaryPath() ->
    // resolveTarget() throws for e.g. Windows arm64, which passes the
    // package.json os/cpu gate but has no published binary.
    console.error(`volcano: could not obtain the CLI binary: ${err.message}`);
    console.error(
      'Install it manually from https://github.com/Kong/volcano-cli/releases ' +
        'or re-install the package.'
    );
    process.exit(1);
  }
}

async function main() {
  const bin = await resolveBinary();
  const child = spawn(bin, process.argv.slice(2), { stdio: 'inherit' });

  child.on('error', (err) => {
    console.error(`volcano: failed to launch binary: ${err.message}`);
    process.exit(1);
  });

  child.on('exit', (code, signal) => {
    if (signal) {
      // Re-raise the signal so the parent reflects the child's termination.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code == null ? 1 : code);
  });
}

main().catch((err) => {
  // Last-resort guard so failures never surface as an unhandled rejection.
  console.error(`volcano: ${err.message}`);
  process.exit(1);
});
