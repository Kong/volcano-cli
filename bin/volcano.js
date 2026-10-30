#!/usr/bin/env node
'use strict';

// Launcher shim: exec the platform-specific Volcano CLI binary, forwarding all
// arguments, stdio, the descriptors the arguments name, and the exit code. If
// the binary is missing (e.g. install ran with --ignore-scripts, or the
// postinstall download failed) it is fetched and verified on demand.

const fs = require('fs');
const { spawn } = require('child_process');
const { assetName, binaryPath, ensureBinary } = require('../scripts/npm/download.js');

async function resolveBinary() {
  try {
    const bin = binaryPath();
    if (fs.existsSync(bin)) {
      return bin;
    }
    return await ensureBinary();
  } catch (err) {
    // Covers download failures and unsupported platforms.
    console.error(`volcano: could not obtain the CLI binary: ${err.message}`);
    const manual = process.platform === 'win32' && ['x64', 'arm64'].includes(process.arch)
      ? `https://download.volcano.dev/builds/releases/latest/download/${assetName()}`
      : 'https://download.volcano.dev/builds/install.sh';
    console.error(`Install it manually from ${manual} or re-install the package.`);
    process.exit(1);
  }
}

// A path such as /dev/fd/63, which a shell's process substitution passes
// (`volcano variables deploy -f <(...)`), names a descriptor this process
// inherited. spawn hands the binary only stdin, stdout and stderr, so each
// descriptor an argument names is passed through at the same number, or the
// binary finds it closed.
const DESCRIPTOR_PATH = /(?:^|=)\/(?:dev\/fd|proc\/self\/fd)\/(\d+)$/;

function isOpen(fd) {
  try {
    fs.fstatSync(fd);
    return true;
  } catch {
    return false;
  }
}

function stdioFor(args) {
  const stdio = ['inherit', 'inherit', 'inherit'];
  if (process.platform === 'win32') {
    return stdio;
  }
  for (const arg of args) {
    const match = DESCRIPTOR_PATH.exec(arg);
    const fd = match ? Number(match[1]) : -1;
    if (fd < 3 || !isOpen(fd)) {
      continue;
    }
    while (stdio.length < fd) {
      stdio.push('ignore');
    }
    stdio[fd] = fd;
  }
  return stdio;
}

async function main() {
  const bin = await resolveBinary();
  const args = process.argv.slice(2);
  const child = spawn(bin, args, { stdio: stdioFor(args) });

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
