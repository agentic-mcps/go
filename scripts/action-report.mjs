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
  const files = report.change?.files ?? [];
  const declarations = report.change?.declarations ?? [];
  const findings = report.findings ?? [];
  const total = (value, visible) => value ?? visible.length;
  const shown = (value, truncated, visible) => `${total(value, visible)}${truncated ? ` (${visible.length} shown)` : ''}`;
  const impact = report.impact?.packages_truncated
    ? `Impact: ${total(report.impact?.packages_total, packages)} packages (${packages.length} shown; visible: ${direct} direct, ${reverse} reverse-dependent)`
    : `Impact: ${direct} direct, ${reverse} reverse-dependent packages`;
  const lines = [`## agentic-go verification: ${result}`, '', report.result?.summary ?? 'No summary provided.', '', `Change: ${shown(report.change?.files_total, report.change?.files_truncated, files)} files, ${shown(report.change?.declarations_total, report.change?.declarations_truncated, declarations)} declarations`, impact, `Evidence: ${(report.evidence ?? []).map((item) => `${item.kind}:${item.status}`).join(', ') || 'none'}`, `Findings: ${shown(report.findings_total, report.findings_truncated, findings)}`, `Risks: ${report.risks?.length ?? 0}`, `Uncertainties: ${report.uncertainties?.length ?? 0}`];
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
