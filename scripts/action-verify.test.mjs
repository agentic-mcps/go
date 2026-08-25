import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const actionPath = fileURLToPath(new URL('..', import.meta.url));
const verifier = path.join(actionPath, 'scripts', 'action-verify.sh');
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentic-verify-action-'));
const bin = path.join(dir, 'agentic-go');
const calls = path.join(dir, 'calls');
fs.writeFileSync(bin, `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$AGENTIC_FAKE_CALLS"
printf '%s\\n' "$AGENTIC_FAKE_REPORT"
exit "$AGENTIC_FAKE_EXIT"
`, { mode: 0o755 });
const baseEnv = {
  ...process.env,
  PATH: `${dir}${path.delimiter}${process.env.PATH}`,
  RUNNER_TEMP: dir,
  GITHUB_ACTION_PATH: actionPath,
  GITHUB_OUTPUT: path.join(dir, 'output'),
  GITHUB_STEP_SUMMARY: path.join(dir, 'summary'),
  AGENTIC_FAKE_CALLS: calls,
};
const report = (status) => JSON.stringify({
  result: { status, summary: status },
  change: { files: [], declarations: [] },
  impact: { packages: [] },
  evidence: [], findings: [], risks: [], uncertainties: [],
});
const run = (status, exitCode, enforce, extra = {}) => spawnSync('bash', [verifier, 'origin/main', './...', extra.race ?? 'false', 'error', extra.coverage ?? '', '200', enforce], {
  env: { ...baseEnv, AGENTIC_FAKE_REPORT: report(status), AGENTIC_FAKE_EXIT: String(exitCode) },
  encoding: 'utf8',
});

let result = run('findings', 1, 'false');
assert.equal(result.status, 0, result.stderr);
assert.equal(fs.readFileSync(calls, 'utf8').trim().split('\n').length, 1);
assert.match(fs.readFileSync(calls, 'utf8'), /^verify --base origin\/main --package \.\/\.\.\. --format json /);

fs.writeFileSync(calls, '');
result = run('findings', 1, 'true', { race: 'true', coverage: '80' });
assert.equal(result.status, 1);
assert.equal(fs.readFileSync(calls, 'utf8').trim().split('\n').length, 1);
assert.match(fs.readFileSync(calls, 'utf8').trim(), /--race --min-changed-coverage 80$/);

fs.writeFileSync(calls, '');
result = run('incomplete', 2, 'false');
assert.equal(result.status, 0, result.stderr);
assert.equal(fs.readFileSync(calls, 'utf8').trim().split('\n').length, 1);

result = run('pass', 0, 'maybe');
assert.equal(result.status, 2);
console.log('action verify adapter tests passed');
