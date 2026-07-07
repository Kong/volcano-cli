#!/usr/bin/env node
'use strict';

// npm postinstall hook: download the platform-specific Volcano CLI binary.
//
// Failures here are non-fatal so that `npm install` still succeeds in
// restricted/offline environments (or with `--ignore-scripts`). In that case
// the launcher shim (bin/volcano.js) downloads the binary on first use.

const { ensureBinary, resolveTarget } = require('./download.js');
const pkg = require('../../package.json');

async function main() {
  if (process.env.VOLCANO_SKIP_DOWNLOAD === '1') {
    console.log('Skipping Volcano CLI download (VOLCANO_SKIP_DOWNLOAD=1).');
    return;
  }

  // No release exists for the placeholder version, so skip quietly for anyone
  // running `npm install` in a source checkout (the publish workflow stamps a
  // real version before publishing).
  if (pkg.version === '0.0.0') {
    console.log('Skipping Volcano CLI download for development version 0.0.0.');
    return;
  }

  const target = resolveTarget();
  const binary = await ensureBinary();
  console.log(`Installed Volcano CLI (${target}) -> ${binary}`);
}

main().catch((err) => {
  console.warn(`Warning: could not download the Volcano CLI binary: ${err.message}`);
  console.warn('It will be downloaded automatically the first time you run "volcano".');
  // Exit 0 so installation does not hard-fail; the shim self-heals on first run.
  process.exit(0);
});
