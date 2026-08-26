# v0.2.0 change-verification evidence

This directory records local, pre-publication evidence for the v0.2.0 change-
verification implementation. It is implementation evidence, not an adoption,
retention, product-market-fit, or universal-correctness claim.

The final CLI and MCP dogfood below used commit
`dc5d6b3a30ff801cba3d267f223d5c12681add43`. The final release matrix used
`6bc7d8bef3b195a22172354266f43d3c59a7056a`. All work used the configured
Ashwin Gopalsamy identity on branch `feat/v0.2.0`. No tag, push, transfer, or
persistent MCP client configuration was created.

## Contract goldens

Three checked-in reports exercise the complete policy-state vocabulary:

- [`report-pass.json`](../../internal/verification/testdata/report-pass.json)
  is a representative clean report with a changed declaration, direct and
  reverse-dependent packages, four checks, test and changed-coverage evidence,
  analyzer comparisons, one risk area, and one uncertainty;
- [`report-findings.json`](../../internal/verification/testdata/report-findings.json)
  contains an introduced calibrated analyzer finding and exit `1`; and
- [`report-incomplete.json`](../../internal/verification/testdata/report-incomplete.json)
  contains unavailable required evidence and exit `2`.

The golden test rejects unknown fields, requires non-null collections, checks
status/exit consistency, and requires finalization to preserve the entire
already ordered report before byte-for-byte canonical JSON comparison:

```sh
GOTOOLCHAIN=local go test ./internal/verification \
  -run TestGoldenReportsMatchPortableContract
```

All three fixtures also validated against
[`agentic.verify/v1alpha1`](../../docs/schema/archive/verification-report-v1alpha1.json)
with the repository's existing JSON Schema 2020-12 implementation.

## Agentic-go dogfood

### CLI

The release candidate was built and asked to verify its own complete v0.2
change against the local `v0.1.0` tag:

```sh
go127=/path/to/go1.27.0/bin/go
run_dir="$(mktemp -d)"
GOTOOLCHAIN=local "$go127" build -o "$run_dir/agentic-go" ./cmd/agentic-go
PATH="$(dirname "$go127"):$PATH" GOTOOLCHAIN=local \
  /usr/bin/time -p "$run_dir/agentic-go" verify \
    --workspace "$PWD" \
    --base v0.1.0 \
    --package ./... \
    --format json >"$run_dir/report.json"
```

The process and report exited `0` in 21.39 seconds. The report identified:

| Evidence | Observed result |
| --- | --- |
| Repository | base/merge-base `b62b73c8153966508519d08aff891b06c2a1f910`; HEAD `dc5d6b3a30ff801cba3d267f223d5c12681add43`; clean snapshot |
| Changed files | 15 shown / 63 total; explicitly truncated |
| Changed declarations | 20 shown / 601 total; explicitly truncated |
| Affected packages | 10 shown / 10 total; not truncated |
| Tests | 149 passed, 0 failed, 0 skipped across 10 packages |
| Changed coverage | 1,732 / 2,158 statements, 80.2595%; 20 / 389 uncovered ranges shown |
| Analyzer baseline | concurrency and errors both 0 base, 0 current, 0 introduced, 0 existing, 0 resolved, 0 unknown |
| Findings | 0 shown / 0 total |
| Risk areas | 7 change-grounded lenses |
| Uncertainties | active build constraints, external consumers, generated code, and unmodelled non-Go change |

The JSON payload was 23,269 bytes, used only workspace-relative source paths,
and contained no null collection. Policy saw the complete impacted closure and
all findings before display bounding. Race evidence was intentionally absent
because this invocation used the default `race=false`; the separate release
matrix ran the race-enabled suite.

### Go SDK stdio client

A one-process stdio probe using the user-supplied local checkout of
`github.com/modelcontextprotocol/go-sdk` connected to the same final binary. It
listed every surface, read every resource, rendered every prompt, ran both
audits, ran `go_test_structured`, and invoked `go_verify_change` once.

| Contract | Observed result |
| --- | --- |
| Inventory | 8 tools, 4 resources, 4 prompts |
| `verify-change` prompt | exactly one `go_verify_change` reference |
| Verification annotations | read-only `false`, destructive `true`, idempotent `false`, open-world `true` |
| Resources | all four returned non-empty content |
| Direct audits | concurrency 0 findings; errors 0 findings |
| Structured tests | 209 passed, 0 failed, 0 skipped; 16 `ok` packages and 1 no-test `skip` package |
| Verification transport | one concise text fallback plus a 23,268-byte canonical `structuredContent` report |
| Report | `pass`, exit `0`, 0 findings, 7 risks, 4 uncertainties; no null collections or absolute path leakage |
| Probe duration | 37.000 seconds |

The standalone probe was deliberately ephemeral rather than a shipped third
adapter. The repository's protocol and registry tests preserve the same
inventory, prompt, annotation, and structured-result assertions.

### Ephemeral Codex MCP client

An ephemeral Codex process was configured only for the command invocation; no
entry was written to persistent client configuration. The prompt required the
client to use only agentic-go, discover the surface, read all four resources,
and call `go_verify_change` exactly once with `base=v0.1.0`, `package=./...`,
`fail_on=error`, `max_packages=200`, and `race=false`.

The client discovered eight tools, read all four resources, and emitted one
tool-call start plus its matching completion. It retained the complete bounded
report: `pass`, exit `0`, 15/63 files, 20/601 declarations, 10/10 packages,
149 passing tests, 80.2595% changed coverage, no findings, seven risks, and
four uncertainties. Searches of the JSONL event stream found no token
truncation marker. All reported source locations were workspace-relative.

The Codex client surface did not expose prompt inventory or tool annotations;
the independent SDK probe verified those fields. Codex did not emit a reliable
whole-call duration; report evidence recorded 1.609 seconds for concurrency
analysis, 1.542 seconds for error analysis, and 11.226 seconds for the shared
test/coverage execution.

## Reviewed historical changes

The three repositories were user-cloned and deepened locally. Evaluation used
fresh temporary clones from those Git objects so dependency-download changes
in the source checkouts could not contaminate the snapshots. Reports and timing
files were written outside each worktree. Every recorded snapshot was clean;
`GOTOOLCHAIN=local`, `GOPROXY=off`, and `GOSUMDB=off` prevented implicit
toolchain or module downloads.

| Project / purpose | Head | Base | Go | Scope | Time | Result |
| --- | --- | --- | --- | --- | ---: | --- |
| grpc-go / reverse impact | `4793ad0474669eafbd5346c8a3d098fdfa542498` | `3284af7e654993f7b9de31996ebc8fa81e375d8a` | 1.27.0 | `./credentials/...` | 6.68 s | `pass` |
| client_golang / changed coverage | `89c60c0554699c74fa3d4eec472b98a3108905e3` | `3df9b67fc3081c645189b48a20e1c86a44ee3021` | 1.26.7 | `./...` | 20.62 s | `pass` |
| Echo / analyzer baseline | `4ec116dc9b5bff1da7513c0dc86095d0e8ce9654` | `05489dc1730161df26b72d1ae2a3ba6fb8178fc7` | 1.27.0 | `./...` | 8.23 s | `pass` |

The exact reports are
[`grpc-go-reverse-impact.json`](grpc-go-reverse-impact.json),
[`client-golang-changed-coverage.json`](client-golang-changed-coverage.json),
and [`echo-analyzer-baseline.json`](echo-analyzer-baseline.json). They contain
no absolute local path and validate against `agentic.verify/v1alpha1`.

### grpc-go: reverse impact and scope cost

The change preserves `boundAccessToken` when `altsTC.Clone` copies transport
credentials and adds a focused regression assertion. The scoped command was:

```sh
agentic-go verify \
  --base 3284af7e654993f7b9de31996ebc8fa81e375d8a \
  --package ./credentials/... \
  --format json
```

The report mapped both modified methods to
`google.golang.org/grpc/credentials/alts`, then added
`google.golang.org/grpc/credentials/google` as a distance-one
`reverse_import`. Four checks ran against both packages. Forty-five tests
passed, but the directly changed ALTS package had no active test cases and the
one changed executable statement was uncovered: changed coverage was 0/1.
Concurrency and error baselines were clean. The report also disclosed active
build constraints and invisible external consumers.

This was materially useful: an ordinary green test summary could suggest the
change was exercised, while the report showed that all 45 executed tests came
from the reverse importer and the changed clone assignment was not covered in
the active configuration. The package scope is an explicit limitation; it does
not cover reverse importers outside `./credentials/...`.

A separate full `./...` run found the complete 58-package reverse closure and
retained 20 visible package details, but the single `go test -json` stream
exceeded the fixed 8 MiB stdout cap. It returned `incomplete` after 37.60
seconds with test and coverage evidence unavailable, while both analyzer
comparisons still completed. That negative sample is preserved as
[`grpc-go-full-scope-incomplete.json`](grpc-go-full-scope-incomplete.json).
The outcome is honest containment, but also a real v0.2 limitation for broad,
high-output closures; callers must narrow package scope rather than receiving a
nearest-package subset.

### client_golang: changed coverage across reverse importers

The change adds `GatherAndFormat` to the Prometheus test utility package and
tests the new helper. The complete-workspace command was:

```sh
agentic-go verify \
  --base 3df9b67fc3081c645189b48a20e1c86a44ee3021 \
  --package ./... \
  --format json
```

The report identified `github.com/prometheus/client_golang/prometheus/testutil`
as directly changed and expanded the closure to 18 packages across three
reverse-import distances. All 379 active tests passed; ten affected packages
correctly reported package status `skip` because they contained no active test
cases. Changed coverage was 3/3 statements, or 100%. The concurrency analyzer
reported one finding in both snapshots and classified it as pre-existing, so
the primary finding list remained empty. Error analysis was clean.

This sample was useful in a different way from grpc-go: it demonstrated that
the added helper statements were executed while also showing which downstream
packages supplied the broader verification evidence. It does not prove the
new assertions are behaviorally complete. The report activated error-flow,
exported-API, and observability review lenses and disclosed the changed
changelog as an unmodelled non-Go input.

The repository revision does not compile its runtime-metrics fixtures under Go
1.27.0, so the recorded sample used supported Go 1.26.7. That is an upstream
revision/toolchain compatibility limitation, not evidence produced by the
change-verification engine.

### Echo: analyzer baseline suppression

The change rejects an unknown-only CSRF `TokenLookup` configuration and adds a
regression test. The complete-workspace command was:

```sh
agentic-go verify \
  --base 05489dc1730161df26b72d1ae2a3ba6fb8178fc7 \
  --package ./... \
  --format json
```

The report mapped both changed declarations to Echo's middleware package. All
583 tests passed and changed coverage was 2/2 statements, or 100%. Concurrency
analysis found the same single diagnostic in the base and current snapshots;
baseline comparison classified it as pre-existing and emitted no introduced
finding. Error analysis was clean. The report retained exported-API and
error-flow review lenses plus uncertainty about external consumers.

This sample directly exercised the v0.2 baseline promise: a valid pre-existing
diagnostic remained visible in comparison metadata without becoming change
noise. It does not establish that every move, rename, or ambiguous diff can be
matched; those cases deliberately become `baseline_unknown`.

### Artifact integrity

| Report | SHA-256 |
| --- | --- |
| `grpc-go-reverse-impact.json` | `27ecf8dba42e1e596114cda5b0791daa5c0f50a9f5536a8839fe64ed7e559371` |
| `grpc-go-full-scope-incomplete.json` | `5379cfd605ba87f605cc7bd442d842a34cd047dfb34d4d48f0b2cc36e2a6f9e7` |
| `client-golang-changed-coverage.json` | `33ac9a5ff38a533fb0ac7838a72e358b96de662e859041a4a95fd71970378b9e` |
| `echo-analyzer-baseline.json` | `a8beb580e316f2c7bce3255d4d613f14e541e33ad6777b60f504b72dad600146` |

## Release matrix

The complete local gate was rerun at
`6bc7d8bef3b195a22172354266f43d3c59a7056a` after the golden-report tests were
added. `GOTOOLCHAIN=local` was set for every Go command.

| Gate | Version / target | Wall time | Result |
| --- | --- | ---: | --- |
| `go test ./...` | Go 1.25.13 | 8.19 s | pass |
| `go test ./...` | Go 1.26.7 | 8.34 s | pass |
| `go test ./...` | Go 1.27.0 | 0.53 s | pass |
| `go test -race ./...` | Go 1.27.0 | 20.60 s | pass |
| `go vet ./...` | Go 1.27.0 | 0.20 s | pass |
| `go build ./...` | Go 1.27.0 | 0.72 s | pass |
| `staticcheck ./...` | Staticcheck 0.7.0, Go 1.26.7 | 0.45 s | pass |
| `golangci-lint run` | golangci-lint 2.12.2, Go 1.27.0 | 0.60 s | 0 issues |
| `scripts/action-*.test.mjs` | Node 24.4.1, five files | 0.07–0.28 s each | pass |
| YAML parse | Ruby Psych, both release YAML files | 0.07 s | pass |
| `goreleaser check` | GoReleaser 2.17.1 | 0.25 s | pass |
| Cross-build | Darwin/Linux × amd64/arm64 × two binaries | not captured | eight pass |

Cross-builds used `CGO_ENABLED=0`; `file` confirmed Mach-O for Darwin and ELF
for Linux for both `agentic-go` and `agentic-go-vet`. The five Action adapter
tests covered base derivation, checksum rejection, release/platform mapping,
summary and source annotations, actual status outputs, exactly one CLI call,
and advisory versus enforced exits.

Every one of the 75 commits reachable from the branch HEAD had a valid local
signature, the configured Ashwin Gopalsamy author, and a Conventional Commit
subject. Auxiliary local refs include unpublished backup/Codex refs and one
remote Dependabot ref, so all-ref author counts are not branch-history counts.
`git fsck --full --no-reflogs --unreachable` completed successfully and listed
15 unreachable commits, 25 blobs, and 35 trees retained from unpublished local
history work. They were not altered in this implementation slice; history
curation remains a separate publication-gate task. `git diff --check` passed,
and the worktree was clean at the end of the matrix before this evidence file
was written.

## Interpretation and limitations

- Affected packages are a conservative reverse-import closure, not proof of
  runtime reachability and not permission to omit packages outside the
  selected scope.
- Changed coverage reports executed changed statements; it does not prove the
  assertions were meaningful or the behavior complete.
- Analyzer baseline matching deliberately prefers `baseline_unknown` over a
  false introduced/pre-existing classification when a diff location is
  ambiguous.
- Risk areas identify changed constructs and review lenses. They are not
  analyzer findings, vulnerabilities, or evidence that an untriggered domain
  is risk-free.
- v0.1's observed 0% false-positive rate remains specific to its pinned,
  reviewed corpus. No analyzer predicate changed in v0.2, so this work did not
  repeat or broaden that calibration scan.
- The historical samples above test whether reports surface useful evidence
  on selected changes. Three examples cannot establish adoption, general
  usefulness, or population-level precision.
