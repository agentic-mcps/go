# Change Verification

Agentic-go produces source-grounded verification reports for Go changes. The
same language is used by the CLI, GitHub Action, MCP adapter, and future
language implementations.

## Language

**Change Request**:
A request to compare a final repository snapshot with an explicit base and
collect verification evidence under a stated policy.
_Avoid_: Task, tool call

**Change Snapshot**:
The committed, staged, unstaged, and untracked content visible for one change
request, identified together with its base and current commit.
_Avoid_: Diff, patch

**Impact**:
The changed and transitively dependent source units that may be affected by a
change snapshot, together with the relationships that justify inclusion.
_Avoid_: Reachability, affected tests

**Verification Plan**:
The ordered checks selected for an impact, with a reason for every check.
_Avoid_: Tool list, workflow

**Evidence**:
The recorded outcome of a check that actually ran, including clean outcomes,
failures, duration, and bounded details.
_Avoid_: Result, proof

**Finding**:
An observed issue produced by executed evidence, such as a failed test, race,
coverage-policy violation, or calibrated analyzer diagnostic.
_Avoid_: Risk, warning

**Risk Area**:
A changed construct that justifies focused review or another check but does not
itself diagnose a defect.
_Avoid_: Finding, vulnerability

**Uncertainty**:
A known limit that prevents the report from supporting a stronger conclusion.
_Avoid_: Error, failure

**Verification Report**:
The complete portable record of a change request, its impact, plan, evidence,
findings, risk areas, uncertainties, and policy result.
_Avoid_: MCP response, test report

**Policy Result**:
The report's automation state: requested evidence passed policy, blocking
findings were observed, or required evidence was incomplete.
_Avoid_: Safety verdict, confidence score

## Bounded report detail

The report evaluates complete evidence and findings before truncating any
display collection. Adapters must preserve and expose each collection's total
and truncation flag; omitted records are not evidence of safety. The full
impacted closure still drives planning and execution before display
truncation. The caps are 15 changed files, 5 base/current ranges per changed
file, 20 declarations, 20 impacted packages, 20 check targets, 20 test package
summaries, 20 nonpassing tests, 50 findings, 5 locations per risk or
uncertainty, and 20 uncovered coverage ranges.
