'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');

const { assetName } = require('./download.js');

test('maps supported Node platforms to published release assets', () => {
  const cases = [
    ['linux', 'x64', 'volcano-linux-amd64'],
    ['linux', 'arm64', 'volcano-linux-arm64'],
    ['darwin', 'x64', 'volcano-macos-amd64'],
    ['darwin', 'arm64', 'volcano-macos-arm64'],
    ['win32', 'x64', 'volcano-windows-amd64.exe'],
  ];

  for (const [platform, arch, expected] of cases) {
    assert.equal(assetName(platform, arch), expected);
  }
});

test('rejects a platform without a published release asset', () => {
  assert.throws(() => assetName('win32', 'arm64'), /Unsupported platform "win32-arm64"/);
});
