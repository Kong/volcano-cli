'use strict';

// Runs the launcher shim against a stand-in binary that prints the file named
// by its --file argument, so these show what the real binary would read.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const { spawn, spawnSync } = require('node:child_process');

const { assetName } = require('../scripts/npm/download.js');

const unix = process.platform !== 'win32';
const fakeEnv = 'API_KEY=fake-value\n';

// A package laid out like the published one, with the stand-in at the path the
// shim resolves.
function fakePackage(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'volcano-shim-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  fs.mkdirSync(path.join(dir, 'bin'));
  fs.mkdirSync(path.join(dir, 'scripts', 'npm'), { recursive: true });
  fs.copyFileSync(path.join(__dirname, '..', 'package.json'), path.join(dir, 'package.json'));
  fs.copyFileSync(path.join(__dirname, 'volcano.js'), path.join(dir, 'bin', 'volcano.js'));
  fs.copyFileSync(path.join(__dirname, '..', 'scripts', 'npm', 'download.js'), path.join(dir, 'scripts', 'npm', 'download.js'));
  fs.writeFileSync(path.join(dir, 'bin', assetName()), [
    '#!/bin/sh',
    'while [ $# -gt 0 ]; do',
    '  case "$1" in',
    '    -f|--file) shift; cat "$1" || exit 3 ;;',
    '    --file=*) cat "${1#--file=}" || exit 3 ;;',
    '    -f?*) cat "${1#-f}" || exit 3 ;;',
    '    --exit=*) exit "${1#--exit=}" ;;',
    '  esac',
    '  shift',
    'done',
    '',
  ].join('\n'), { mode: 0o755 });
  return path.join(dir, 'bin', 'volcano.js');
}

function run(stdio, ...args) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, args, { stdio });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', reject);
    child.on('close', (code) => resolve({ code, stdout, stderr }));
  });
}

// The shim execs the binary where Node has process.execve and spawns it
// otherwise, so each case runs both ways. Spawning can show a descriptor was
// inherited only on Linux; elsewhere it passes none on.
//
// Node marks inherited descriptors below about 20 close-on-exec as it starts,
// so the inherited one here is above that, as bash's process substitution is.
const INHERITED_FD = 40;

function stdioWith(fd) {
  const stdio = ['ignore', 'pipe', 'pipe'];
  while (stdio.length < INHERITED_FD) {
    stdio.push('ignore');
  }
  stdio[INHERITED_FD] = fd;
  return stdio;
}
const modes = [
  { name: 'exec', skip: !unix || typeof process.execve !== 'function', passesDescriptors: true },
  { name: 'spawn', skip: !unix, passesDescriptors: process.platform === 'linux' },
];

// nodeArgs runs the shim, first removing process.execve for the spawn mode.
function nodeArgs(t, mode, shim, ...args) {
  if (mode.name === 'exec') {
    return [shim, ...args];
  }
  const preload = path.join(path.dirname(shim), 'no-execve.js');
  fs.writeFileSync(preload, 'process.execve = undefined;\n');
  return ['--require', preload, shim, ...args];
}

function fakeEnvFile(t, shim) {
  const file = path.join(path.dirname(shim), 'fake.env');
  fs.writeFileSync(file, fakeEnv);
  const fd = fs.openSync(file, 'r');
  t.after(() => fs.closeSync(fd));
  return fd;
}

function assertPassedThrough(mode, result) {
  if (mode.passesDescriptors) {
    assert.equal(result.stderr, '');
    assert.equal(result.code ?? result.status, 0);
    assert.equal(result.stdout, fakeEnv);
  } else {
    assert.equal(result.code ?? result.status, 3);
    assert.equal(result.stdout, '');
  }
}

for (const mode of modes) {
  test(`${mode.name}: passes a process substitution through to the binary`, { skip: mode.skip }, (t) => {
    const shim = fakePackage(t);
    const args = nodeArgs(t, mode, shim).map((arg) => `"${arg}"`).join(' ');
    const result = spawnSync('bash', ['-c', `"${process.execPath}" ${args} -f <(printf '${fakeEnv.trim()}\\n')`], { encoding: 'utf8' });
    assertPassedThrough(mode, result);
  });

  test(`${mode.name}: passes an inherited descriptor named in --file= through to the binary`, { skip: mode.skip }, async (t) => {
    const shim = fakePackage(t);
    const fd = fakeEnvFile(t, shim);
    const result = await run(stdioWith(fd), ...nodeArgs(t, mode, shim, `--file=/dev/fd/${INHERITED_FD}`));
    assertPassedThrough(mode, result);
  });

  test(`${mode.name}: passes a descriptor named after an attached short flag through to the binary`, { skip: mode.skip }, async (t) => {
    const shim = fakePackage(t);
    const fd = fakeEnvFile(t, shim);
    const result = await run(stdioWith(fd), ...nodeArgs(t, mode, shim, `-f/dev/fd/${INHERITED_FD}`));
    assertPassedThrough(mode, result);
  });

  // Node opens its own descriptors at low numbers, so a low one the shim was
  // not given must reach the binary closed, never as one of Node's. So must a
  // low inherited one, which can't be told from Node's.
  test(`${mode.name}: a descriptor the shim was not given stays closed`, { skip: mode.skip }, async (t) => {
    const shim = fakePackage(t);
    for (const fd of [3, 4, 5, 6, 7, 8, 9, 10, 11, 200]) {
      const result = await run(['ignore', 'pipe', 'pipe'], ...nodeArgs(t, mode, shim, `--file=/dev/fd/${fd}`));
      assert.equal(result.code, 3, `descriptor ${fd}`);
      assert.equal(result.stdout, '', `descriptor ${fd}`);
    }
    const inherited = await run(['ignore', 'pipe', 'pipe', 'ignore', fakeEnvFile(t, shim)], ...nodeArgs(t, mode, shim, '--file=/dev/fd/4'));
    assert.equal(inherited.code, 3, 'a low inherited descriptor');
    assert.equal(inherited.stdout, '', 'a low inherited descriptor');
  });

  test(`${mode.name}: passes stdin and the exit code through`, { skip: mode.skip }, async (t) => {
    const shim = fakePackage(t);
    // A file rather than spawn's input option: on Linux that is a socket,
    // which /dev/stdin can't open.
    const fd = fakeEnvFile(t, shim);
    const piped = await run([fd, 'pipe', 'pipe'], ...nodeArgs(t, mode, shim, '--file', '/dev/stdin'));
    assert.equal(piped.stderr, '');
    assert.equal(piped.code, 0);
    assert.equal(piped.stdout, fakeEnv);

    const result = await run(['ignore', 'pipe', 'pipe'], ...nodeArgs(t, mode, shim, '--exit=7'));
    assert.equal(result.code, 7);
  });
}
