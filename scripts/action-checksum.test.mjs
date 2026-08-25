import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agentic-checksum-'));
const archive = path.join(dir, 'agentic-go_0.2.0_linux_amd64.tar.gz');
const checksums = path.join(dir, 'checksums.txt');
const verifier = fileURLToPath(new URL('./action-checksum.sh', import.meta.url));
fs.writeFileSync(archive, 'verified artifact');
const hash = crypto.createHash('sha256').update('verified artifact').digest('hex');
fs.writeFileSync(checksums, `${hash}  ${path.basename(archive)}\n`);

let run = spawnSync('bash', [verifier, checksums, archive], { encoding: 'utf8' });
assert.equal(run.status, 0, run.stderr);

fs.writeFileSync(archive, 'tampered artifact');
run = spawnSync('bash', [verifier, checksums, archive], { encoding: 'utf8' });
assert.equal(run.status, 2);
assert.match(run.stderr, /checksum mismatch/);

fs.writeFileSync(checksums, `${hash}  another.tar.gz\n`);
run = spawnSync('bash', [verifier, checksums, archive], { encoding: 'utf8' });
assert.equal(run.status, 2);
assert.match(run.stderr, /exactly one entry/);

console.log('action checksum tests passed');
