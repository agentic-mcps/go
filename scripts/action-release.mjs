import fs from 'node:fs';

const [versionInput = '', actionRef = '', platformInput = process.platform, archInput = process.arch] = process.argv.slice(2);
const targets = {
  'darwin:x64': ['darwin', 'amd64'],
  'darwin:arm64': ['darwin', 'arm64'],
  'linux:x64': ['linux', 'amd64'],
  'linux:arm64': ['linux', 'arm64'],
};
const target = targets[`${platformInput}:${archInput}`];
if (!target) {
  console.error(`agentic-go: unsupported runner ${platformInput}/${archInput}; supported targets are Darwin/Linux amd64/arm64`);
  process.exit(2);
}

let candidate = versionInput;
if (!candidate) {
  if (!/^v\d+\.\d+\.\d+$/.test(actionRef)) {
    console.error('agentic-go: version is required when the Action ref is not an exact semver tag');
    process.exit(2);
  }
  candidate = actionRef;
}
const version = candidate.replace(/^v/, '');
if (!/^\d+\.\d+\.\d+$/.test(version)) {
  console.error(`agentic-go: invalid release version '${version}'`);
  process.exit(2);
}

const [os, arch] = target;
const values = { version, os, arch, archive: `agentic-go_${version}_${os}_${arch}.tar.gz` };
if (process.env.GITHUB_OUTPUT) {
  fs.appendFileSync(process.env.GITHUB_OUTPUT, Object.entries(values).map(([key, value]) => `${key}=${value}`).join('\n') + '\n');
} else {
  process.stdout.write(JSON.stringify(values));
}
