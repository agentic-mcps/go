import fs from 'node:fs';
const [reportPath, exitText, enforceText] = process.argv.slice(2);
let report;
try { report = JSON.parse(fs.readFileSync(reportPath, 'utf8')); }
catch (error) { console.error(`agentic-go: invalid or unreadable JSON report: ${error.message}`); process.exit(3); }
const result = report.result?.status;
const exitCode = Number(exitText);
const expected = { pass: 0, findings: 1, incomplete: 2 }[result];
if (expected === undefined || !Number.isInteger(exitCode) || exitCode !== expected) { console.error(`agentic-go: report status '${result ?? ''}' is inconsistent with CLI exit ${exitText}`); process.exit(3); }
if (enforceText !== 'true' && enforceText !== 'false') { console.error('agentic-go: enforce must be true or false'); process.exit(3); }
const enforce = enforceText === 'true';
if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `report-path=${reportPath}\nstatus=${result}\n`);
if (process.env.GITHUB_STEP_SUMMARY) {
  const packages = report.impact?.packages ?? [];
  const direct = packages.filter((item) => item.distance === 0).length;
  const reverse = packages.filter((item) => item.distance > 0).length;
  const lines = [`## agentic-go verification: ${result}`, '', report.result?.summary ?? 'No summary provided.', '', `Change: ${report.change?.files?.length ?? 0} files, ${report.change?.declarations?.length ?? 0} declarations`, `Impact: ${direct} direct, ${reverse} reverse-dependent packages`, `Evidence: ${(report.evidence ?? []).map((item) => `${item.kind}:${item.status}`).join(', ') || 'none'}`, `Findings: ${report.findings?.length ?? 0}`, `Risks: ${report.risks?.length ?? 0}`, `Uncertainties: ${report.uncertainties?.length ?? 0}`];
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, lines.join('\n') + '\n');
}
const commandEscape = (value) => String(value).replace(/%/g, '%25').replace(/\r/g, '%0D').replace(/\n/g, '%0A');
const propertyEscape = (value) => commandEscape(value).replace(/:/g, '%3A').replace(/,/g, '%2C');
for (const finding of report.findings ?? []) {
  const loc = finding.location;
  const level = finding.severity === 'error' ? 'error' : finding.severity === 'info' ? 'notice' : 'warning';
  const suffix = loc ? `,file=${propertyEscape(loc.file)},line=${propertyEscape(loc.line)}${loc.col ? `,col=${propertyEscape(loc.col)}` : ''}` : '';
  process.stdout.write(`::${level}${suffix}::${commandEscape(finding.message)}\n`);
}
if (enforce && (exitCode === 1 || exitCode === 2)) process.exitCode = exitCode;
