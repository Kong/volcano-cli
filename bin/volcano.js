#!/usr/bin/env node
'use strict';

// Launcher shim: exec the platform-specific Volcano CLI binary, forwarding all
// arguments, stdio, the inherited descriptors the arguments name, and the exit
// code. If
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
// inherited. The path may be a whole argument, follow `=` as in
// --file=/dev/fd/63, or follow a short flag as in -f/dev/fd/63.
const DESCRIPTOR_PATH = /(?:^|=|^-[A-Za-z])\/(?:dev\/fd|proc\/self\/fd)\/(\d+)$/;

// O_CLOEXEC as /proc/self/fdinfo reports it in a descriptor's flags.
const O_CLOEXEC = 0o2000000;

// inherited reports whether fd came from the process that started this one.
// Node opens each of its own descriptors close-on-exec and an inherited one is
// not, but only Linux shows that flag (through /proc/self/fdinfo), so
// elsewhere nothing counts as inherited. Node also marks the low inherited
// descriptors (below about 20) close-on-exec as it starts, so those can't be
// told from its own and aren't passed: bash's process substitution (63 and
// down) gets through, zsh's (11 and up) doesn't.
function inherited(fd) {
  try {
    const flags = /^flags:\s*([0-7]+)$/m.exec(fs.readFileSync(`/proc/self/fdinfo/${fd}`, 'utf8'));
    return flags !== null && (parseInt(flags[1], 8) & O_CLOEXEC) === 0;
  } catch {
    return false;
  }
}

// stdioFor maps each inherited descriptor an argument names to the same number
// in the binary, since spawn hands it only stdin, stdout and stderr otherwise.
// One the launcher can't show was inherited isn't passed, so the binary finds
// it closed rather than reading one of Node's own.
function stdioFor(args) {
  const stdio = ['inherit', 'inherit', 'inherit'];
  for (const arg of args) {
    const match = DESCRIPTOR_PATH.exec(arg);
    const fd = match ? Number(match[1]) : -1;
    if (fd < 3 || !inherited(fd)) {
      continue;
    }
    while (stdio.length < fd) {
      stdio.push('ignore');
    }
    stdio[fd] = fd;
  }
  return stdio;
}

// execBinary replaces this process with the binary where Node can (22.15 and
// 23.11 on, not on Windows). Every descriptor still open without close-on-exec
// was inherited, so exec passes exactly those and closes Node's own, with the
// same limit for low descriptors as inherited. The binary's exit code and
// signals then reach the caller directly. It returns only when it can't exec.
function execBinary(bin, args) {
  if (process.platform === 'win32' || typeof process.execve !== 'function') {
    return;
  }
  try {
    process.execve(bin, [bin, ...args], process.env);
  } catch (err) {
    console.error(`volcano: failed to launch binary: ${err.message}`);
    process.exit(1);
  }
}

async function main() {
  const bin = await resolveBinary();
  const args = process.argv.slice(2);
  execBinary(bin, args);
  const child = spawn(bin, args, { stdio: process.platform === 'win32' ? 'inherit' : stdioFor(args) });

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
