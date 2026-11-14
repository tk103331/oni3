#!/usr/bin/env node
'use strict';

const { spawnSync } = require('child_process');

let bin;
try {
  bin = require('../lib/install').ensureBinarySync();
} catch (err) {
  console.error(`oni3: ${err.message}`);
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error(`oni3: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
