import fs from 'node:fs';
import { spawnSync } from 'node:child_process';

const [explicit = '', eventPath = '', baseRef = ''] = process.argv.slice(2);
let base = explicit;
if (!base && eventPath) {
  let event;
  try { event = JSON.parse(fs.readFileSync(eventPath, 'utf8')); }
  catch (error) { console.error(`agentic-go: cannot read the GitHub event payload: ${error.message}`); process.exit(2); }
  base = event.pull_request?.base?.sha ?? '';
}
if (!base && baseRef) {
  const resolved = spawnSync('git', ['rev-parse', '--verify', '--end-of-options', `origin/${baseRef}^{commit}`], { encoding: 'utf8' });
  if (resolved.status === 0) base = resolved.stdout.trim();
}
if (!base) {
  console.error('agentic-go: base is required (set base or run on a pull_request event with sufficient checkout history)');
  process.exit(2);
}
if (/\s|[\u0000-\u001f\u007f]/u.test(base) || base.startsWith('-')) {
  console.error('agentic-go: resolved base is not a valid local commit or ref');
  process.exit(2);
}
if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `base=${base}\n`);
else process.stdout.write(base);
