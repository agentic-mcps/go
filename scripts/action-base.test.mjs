import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const resolver = fileURLToPath(new URL('./action-base.mjs', import.meta.url));
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentic-base-'));
const event = path.join(dir, 'event.json');
fs.writeFileSync(event, JSON.stringify({ pull_request: { base: { sha: '0123456789abcdef' } } }));
const resolve = (...args) => spawnSync(process.execPath, [resolver, ...args], { encoding: 'utf8' });

let run = resolve('origin/main', event, 'ignored');
assert.equal(run.status, 0, run.stderr);
assert.equal(run.stdout, 'origin/main');
run = resolve('', event, 'ignored');
assert.equal(run.status, 0, run.stderr);
assert.equal(run.stdout, '0123456789abcdef');
run = resolve('', '', '');
assert.equal(run.status, 2);
run = resolve('main\nstatus=injected', '', '');
assert.equal(run.status, 2);
console.log('action base resolution tests passed');
