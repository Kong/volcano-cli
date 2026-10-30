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

test('passes a process substitution through to the binary', { skip: !unix }, (t) => {
  const shim = fakePackage(t);
  const result = spawnSync('bash', ['-c', `"${process.execPath}" "$0" -f <(printf '${fakeEnv.trim()}\\n')`, shim], { encoding: 'utf8' });
  assert.equal(result.stderr, '');
  assert.equal(result.status, 0);
  assert.equal(result.stdout, fakeEnv);
});

test('passes an inherited descriptor named in --file= through to the binary', { skip: !unix }, async (t) => {
  const shim = fakePackage(t);
  const file = path.join(path.dirname(shim), 'fake.env');
  fs.writeFileSync(file, fakeEnv);
  const fd = fs.openSync(file, 'r');
  t.after(() => fs.closeSync(fd));

  const result = await run(['ignore', 'pipe', 'pipe', 'ignore', fd], shim, '--file=/dev/fd/4');
  assert.equal(result.stderr, '');
  assert.equal(result.code, 0);
  assert.equal(result.stdout, fakeEnv);
});

test('passes a descriptor named after an attached short flag through to the binary', { skip: !unix }, async (t) => {
  const shim = fakePackage(t);
  const file = path.join(path.dirname(shim), 'fake.env');
  fs.writeFileSync(file, fakeEnv);
  const fd = fs.openSync(file, 'r');
  t.after(() => fs.closeSync(fd));

  const result = await run(['ignore', 'pipe', 'pipe', 'ignore', fd], shim, '-f/dev/fd/4');
  assert.equal(result.stderr, '');
  assert.equal(result.code, 0);
  assert.equal(result.stdout, fakeEnv);
});

// Node's own descriptors sit at low numbers, so this uses one above them: a
// low number the shim was not given can't be told from one of Node's (see the
// limit in volcano.js).
test('a descriptor above Node\'s own that the shim was not given stays closed', { skip: !unix }, async (t) => {
  const shim = fakePackage(t);
  const result = await run(['ignore', 'pipe', 'pipe'], shim, '--file=/dev/fd/200');
  assert.equal(result.code, 3);
  assert.equal(result.stdout, '');
});

test('passes stdin and the exit code through', { skip: !unix }, async (t) => {
  const shim = fakePackage(t);
  // A file rather than spawn's input option: on Linux that is a socket, which
  // /dev/stdin can't open.
  const file = path.join(path.dirname(shim), 'fake.env');
  fs.writeFileSync(file, fakeEnv);
  const fd = fs.openSync(file, 'r');
  t.after(() => fs.closeSync(fd));
  const piped = await run([fd, 'pipe', 'pipe'], shim, '--file', '/dev/stdin');
  assert.equal(piped.stderr, '');
  assert.equal(piped.code, 0);
  assert.equal(piped.stdout, fakeEnv);

  const result = await run(['ignore', 'pipe', 'pipe'], shim, '--exit=7');
  assert.equal(result.code, 7);
});
