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

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const { spawn } = require('node:child_process');

for (const corrupt of [false, true]) {
  test(`first-party download ${corrupt ? 'preserves the installed binary on checksum failure' : 'installs without GitHub'}`, async (t) => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'volcano-npm-'));
    t.after(() => fs.rmSync(root, { recursive: true, force: true }));
    fs.mkdirSync(path.join(root, 'scripts/npm'), { recursive: true });
    fs.mkdirSync(path.join(root, 'bin'));
    fs.copyFileSync(path.join(__dirname, 'download.js'), path.join(root, 'scripts/npm/download.js'));
    fs.writeFileSync(path.join(root, 'package.json'), JSON.stringify({ version: '1.2.3' }));
    const name = assetName();
    const dest = path.join(root, 'bin', name);
    fs.writeFileSync(dest, 'old binary');
    const payload = 'new binary';
    const checksum = crypto.createHash('sha256').update(corrupt ? 'wrong' : payload).digest('hex');
    const requests = [];
    const server = http.createServer((req, res) => {
      requests.push(req.url);
      if (req.url === '/builds/releases/download/v1.2.3/SHA256SUMS') res.end(`${checksum}  ${name}\n`);
      else if (req.url === `/builds/releases/download/v1.2.3/${name}`) res.end(payload);
      else { res.statusCode = 404; res.end(); }
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    t.after(() => new Promise(resolve => server.close(resolve)));
    // Intercept only the first-party host. A GitHub dependency fails the test.
    const script = `
      require('https').get = (url, options, callback) => {
        const parsed = new URL(url);
        if (parsed.origin !== 'https://download.volcano.dev') throw Error('unexpected host: ' + parsed.origin);
        return require('http').get('http://127.0.0.1:${server.address().port}' + parsed.pathname, options, callback);
      };
      require('./scripts/npm/download.js').ensureBinary({force:true}).catch(e => {console.error(e.message);process.exitCode=1});
    `;
    const env = { ...process.env };
    delete env.VOLCANO_CLI_RELEASES_URL;
    delete env.VOLCANO_GITHUB_RELEASES_URL;
    const child = spawn(process.execPath, ['-e', script], { cwd: root, env });
    let stderr = '';
    child.stderr.on('data', chunk => { stderr += chunk; });
    const code = await new Promise(resolve => child.on('close', resolve));
    assert.equal(code, corrupt ? 1 : 0, stderr);
    if (corrupt) assert.match(stderr, /Checksum mismatch/);
    assert.equal(fs.readFileSync(dest, 'utf8'), corrupt ? 'old binary' : payload);
    assert.equal(requests.length, 2);
    assert.deepEqual(fs.readdirSync(path.join(root, 'bin')), [name]);
  });
}
