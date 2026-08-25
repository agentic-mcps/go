import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const resolver = fileURLToPath(new URL('./action-release.mjs', import.meta.url));
const resolve = (...args) => spawnSync(process.execPath, [resolver, ...args], { encoding: 'utf8' });
for (const [platform, arch, wantOS, wantArch] of [
  ['darwin', 'x64', 'darwin', 'amd64'],
  ['darwin', 'arm64', 'darwin', 'arm64'],
  ['linux', 'x64', 'linux', 'amd64'],
  ['linux', 'arm64', 'linux', 'arm64'],
]) {
  const run = resolve('', 'v0.2.0', platform, arch);
  assert.equal(run.status, 0, run.stderr);
  const value = JSON.parse(run.stdout);
  assert.equal(value.os, wantOS);
  assert.equal(value.arch, wantArch);
  assert.equal(value.archive, `agentic-go_0.2.0_${wantOS}_${wantArch}.tar.gz`);
}
let run = resolve('v0.2.1', '0123456789abcdef', 'linux', 'x64');
assert.equal(run.status, 0, run.stderr);
assert.equal(JSON.parse(run.stdout).version, '0.2.1');
run = resolve('', '0123456789abcdef', 'linux', 'x64');
assert.equal(run.status, 2);
run = resolve('', 'v0.2.0', 'win32', 'x64');
assert.equal(run.status, 2);
run = resolve('latest', 'v0.2.0', 'linux', 'x64');
assert.equal(run.status, 2);
console.log('action release resolution tests passed');
