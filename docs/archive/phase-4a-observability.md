# Domain 4 — Observability (`go_audit_observability`)

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

The original 20-rule security/observability corpus is reproduced here and in
the security domain specification. This file claims only the
logging/tracing/metrics-debuggability subset per `phase-4a-index.md`'s partition rule
("is this a leak risk" → security, "can an operator debug from logs" → here).
Extends `Finding`/`AuditResult`/`Severity`/`Location` and the pass skeleton
from `contracts.md`. Does not redefine them.

## Rules claimed vs. not claimed

Source rules 1–11 (Stringer/`LogValue` masking, `crypto/rand`, custom crypto,
primitive choice, `ConstantTimeCompare`, credentials in source/logs, secrets
manager sourcing, input validation depth, parameterized queries, dependency
scanning) are unambiguously Domain 3's — not mentioned further here.

Three source rules are genuinely ambiguous or explicitly cross-domain; noted
once, not re-litigated per-rule below:

| Source rule | Text | Owning domain | Why |
|---|---|---|---|
| 12 | "Never log AND return the same error" | **Errors** (`errors-04` in `phase-4a-errors.md`) | Explicitly marked in the source itself as "duplicated from error-handling" — it's a duplicate-handling defect, not a logging-shape or leak question. |
| 17 | "Log specific fields, never whole structs" | **Security** | Source frames it entirely as a leak vector (Trap #1, Production Note: 12 months of live OAuth tokens in a log aggregator from a whole-struct `%v`). The "queryability" angle is secondary to the leak angle. |
| 19 | "Audit-log security-relevant state changes" | **Security** | Answers "can an investigation reconstruct this" (compliance/forensics), not "can an operator debug a live issue from logs." |

Source rules 13, 14, 15, 16, 18, 20 are this domain's. 14 and 16 each bundle
multiple distinct imperatives — split into atomic claims below, one row per
claim, per the "distinct rule" test in `phase-4a-index.md` (distinctness is about
content, not source line number).

## Shared helpers

Package `observability` (`internal/analysis/observability/observability.go`)
— directory name matches package name, per `contracts.md`'s Naming
section, never `package analysis`. `isQualifiedCall` and `stringLitValue` no
longer exist anywhere in this domain: both are centralized in
`internal/analysis/astutil/` as `astutil.IsPkgFunc` and `astutil.StringLit`,
used directly at every call site below and in section 2. `enclosingFuncDecl`
is not in `astutil`'s exported surface (`RegisterRule`, `RuleSeverity`,
`RuleName`, `RulesInDomain`, `Report`, `IsPkgFunc`, `IsMethodOn`,
`ExprStmtCall`, `StringLit`, `FuncName`, `FilesScanned` — see
`contracts.md`'s conformance block), so — same as `phase-4a-errors.md`'s
`errors` subpackage — this domain declares its own copy, not a shared one:
each domain subpackage is now its own compilation unit, not a shared
`package analysis` file split across `*.go` siblings.

New helpers specific to this domain (names checked against `astutil`'s and
`phase-4a-errors.md`'s documented lists to avoid collision), plus the domain-local
`enclosingFuncDecl`/`assignmentContext` stack-walkers and the `init()` rule
registration every domain subpackage must declare:

```go
package observability // never package analysis — see contracts.md Naming

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
	astutil.RegisterRule("observability-01", "unstructured_logging", finding.SeverityWarning)
	astutil.RegisterRule("observability-02", "missing_context_in_log_call", finding.SeverityWarning)
	astutil.RegisterRule("observability-03", "duplicate_default_logger_config", finding.SeverityError)
	astutil.RegisterRule("observability-05", "missing_trace_context_propagation", finding.SeverityWarning)
	astutil.RegisterRule("observability-07", "high_cardinality_metric_label", finding.SeverityError)
}

// enclosingFuncDecl walks stack (as supplied by insp.WithStack) outward and
// returns the nearest *ast.FuncDecl, or nil if the call site has no
// enclosing function (a package-level var initializer). Domain-local: not
// part of astutil's exported surface, same as errors.go's copy.
func enclosingFuncDecl(stack []ast.Node) *ast.FuncDecl {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}

// assignmentContext walks stack (as supplied by insp.WithStack) outward from
// a *ast.CallExpr and returns the identifier name the call's result is
// assigned to (single-LHS `:=`/`=` only) and the nearest enclosing
// *ast.BlockStmt. Returns ("", block) if the call is a bare expression
// statement or a multi-value destructure with a non-ident first LHS —
// callers treat that as "no tracked variable," not an error.
func assignmentContext(stack []ast.Node) (string, *ast.BlockStmt) {
	var block *ast.BlockStmt
	for i := len(stack) - 1; i >= 0; i-- {
		if b, ok := stack[i].(*ast.BlockStmt); ok && block == nil {
			block = b
		}
		if assign, ok := stack[i].(*ast.AssignStmt); ok && len(assign.Lhs) >= 1 {
			if id, ok := assign.Lhs[0].(*ast.Ident); ok {
				return id.Name, block
			}
		}
	}
	return "", block
}

// isTestFile reports whether pos lies in a _test.go file. slog.SetDefault
// and fmt.Print* in tests are normal (output capture, ad-hoc debug) and must
// not be flagged.
func isTestFile(pass *analysis.Pass, pos token.Pos) bool {
	return strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go")
}

// hasContextInScope reports whether fn declares a context.Context parameter
// — i.e. whether a *Context slog variant was actually available to the call.
func hasContextInScope(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		if t := pass.TypesInfo.TypeOf(field.Type); t != nil && t.String() == "context.Context" {
			return true
		}
	}
	return false
}

// firstArgIsOSStream reports whether call's first argument is os.Stdout or
// os.Stderr — narrows Fprintf/Fprintln to console/log output, excluding
// arbitrary io.Writer targets (response bodies, files) which are legitimate.
func firstArgIsOSStream(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// requestVarGetsWithContext reports whether reqName is later fed through a
// `.WithContext(` call anywhere in block — the accepted alternative to
// NewRequestWithContext.
func requestVarGetsWithContext(pass *analysis.Pass, block *ast.BlockStmt, reqName string) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "WithContext" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == reqName {
				found = true
			}
		}
		return true
	})
	return found
}

var highCardinalityLabelSubstrings = []string{
	"user_id", "request_id", "session_id", "trace_id", "task_id",
	"job_id", "document_id", "email", "ip_address", "token",
}

func matchesHighCardinalityLabel(name string) bool {
	lower := strings.ToLower(name)
	for _, bad := range highCardinalityLabelSubstrings {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}
```

`Analyzer` (declared in section 4's Analysis subpackage code) is the only
exported symbol from this subpackage — every helper above is unexported,
per `contracts.md`'s Naming section.

---

## 1. Rules table

| Rule ID | Source rule | Status | Severity |
|---|---|---|---|
| `observability-01` | 13 — mandatory `log/slog`, no `fmt.Print*`/`log.Print*` | Implemented | Warning |
| `observability-02` | 15 — always pass context (`*Context` slog variants) | Implemented | Warning |
| `observability-03` | 14 (partial) — configure the default logger exactly once at startup | Implemented | Error |
| `observability-04` | 14 (remainder) — env-driven handler/level choice, logger-via-constructor injection, `slog.With`/`Group` extraction, hot-path log sampling | **Excluded** | — |
| `observability-05` | 16 (partial) — propagate trace-context header across service calls | Implemented | Warning |
| `observability-06` | 16 (remainder) — span-scoping judgment, structured-vs-free-form span attribute shape | **Excluded** | — |
| `observability-07` | 18 — metric label cardinality (RED/USE, stable dimensions only) | Implemented | Error |
| `observability-08` | 20 — separate liveness from readiness | **Excluded** | — |

5 implemented, 3 excluded. Numbering is domain-local and sequential (this
source is shared with Domain 3, so global source numbering isn't preserved
here the way `phase-4a-errors.md` preserves its single-topic source's numbering).

---

## 2. Per-rule AST pattern

### observability-01 — structured logging mandate

**Source:** "`log/slog` for all structured logging. Never `fmt.Printf`,
`log.Println`, or `fmt.Fprintf(os.Stderr, ...)`. Unstructured output cannot
be queried or alerted on."

**Checkable:** yes — closed set of banned call targets (`fmt.Printf/Println/Print`,
`log.Printf/Println/Print`, `fmt.Fprintf/Fprintln` when the writer is
`os.Stdout`/`os.Stderr`), resolved via `go/types` package identity, not string
matching on the identifier `fmt`/`log` (handles local shadowing correctly).

**Predicate** (`*ast.CallExpr`):

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	if isTestFile(pass, call.Pos()) {
		return
	}
	for _, m := range []struct{ pkg, fn string }{
		{"fmt", "Printf"}, {"fmt", "Println"}, {"fmt", "Print"},
		{"log", "Printf"}, {"log", "Println"}, {"log", "Print"},
		{"fmt", "Fprintf"}, {"fmt", "Fprintln"},
	} {
		if !astutil.IsPkgFunc(pass, call, m.pkg, m.fn) {
			continue
		}
		if (m.fn == "Fprintf" || m.fn == "Fprintln") && !firstArgIsOSStream(call) {
			continue // arbitrary io.Writer, not a log/console sink
		}
		astutil.Report(pass, call.Pos(), "observability-01", "unstructured logging via %s.%s, use log/slog instead", m.pkg, m.fn)
	}
})
```

**Exclusions:** `_test.go` files (t.Log/output-capture via `fmt.Print*` is
routine, not a production log path); `fmt.Fprintf`/`Fprintln` only flagged
when the writer literal is `os.Stdout`/`os.Stderr` — any other `io.Writer`
(response body, file, buffer) is a legitimate non-logging use.

**Finding.Message:** `"unstructured logging via %s.%s, use log/slog instead"` (pkg, func)

**Finding.Severity:** Warning. Doesn't break the current request — the call
still runs — but the output is unqueryable and unalertable, which degrades
every future incident response that needs this code path's logs.

---

### observability-02 — pass context for trace correlation

**Source:** "Always pass context: `slog.InfoContext` / `slog.ErrorContext`.
The context carries the trace ID that correlates logs across services."

**Checkable:** yes, with one semantic guard. Syntactic shape: a call to
`slog.Info`/`Warn`/`Error`/`Debug` (package-level) or `.Info`/`.Warn`/`.Error`/`.Debug`
on a `*slog.Logger` receiver, where the exact method name (not `InfoContext`
etc.) is one of the four. Semantic guard: only fires when a `context.Context`
was actually available to pass — otherwise this degenerates into flagging
every `main()`/`init()`-adjacent startup log, which is a legitimate case with
no request context yet.

**Predicate** (`*ast.CallExpr`, walked with `insp.WithStack` to resolve the
enclosing `*ast.FuncDecl`):

```go
insp.WithStack([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
	if !push {
		return true
	}
	call := n.(*ast.CallExpr)
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return true
	}
	switch sel.Sel.Name {
	case "Info", "Warn", "Error", "Debug":
	default:
		return true
	}
	isPkgLevel := astutil.IsPkgFunc(pass, call, "log/slog", sel.Sel.Name)
	isLoggerMethod := astutil.IsMethodOn(pass, call, "log/slog", "Logger", sel.Sel.Name)
	if !isPkgLevel && !isLoggerMethod {
		return true
	}
	if !hasContextInScope(pass, enclosingFuncDecl(stack)) {
		return true // no ctx in scope — nothing to pass, don't flag
	}
	astutil.Report(pass, call.Pos(), "observability-02", "%s call without context, use %sContext for trace correlation", sel.Sel.Name, sel.Sel.Name)
	return true
})
```

**Exclusions:** method names ending in `Context` never match the `switch`
(different identifier, not a suffix check — so `InfoContext` is structurally
excluded, not filtered post-hoc); no flag when the enclosing function has no
`context.Context` parameter in scope.

**Finding.Message:** `"%s call without context, use %sContext for trace correlation"` (level, level)

**Finding.Severity:** Warning. The log line itself is correct and still
lands; the cost is purely at incident time — this request's logs can't be
joined to the distributed trace, which is exactly the debugging cost that
matters when work fans out across independent input sources.

---

### observability-03 — configure the default logger exactly once

**Source:** "Configure `slog` once at startup... Call `slog.SetDefault` once
at program start."

**Checkable:** yes — pure call-count over the pass's `*ast.CallExpr` set, no
type ambiguity (`slog.SetDefault` has one signature, one meaning).

**Predicate:**

```go
var setDefaults []*ast.CallExpr
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	if isTestFile(pass, call.Pos()) || !astutil.IsPkgFunc(pass, call, "log/slog", "SetDefault") {
		return
	}
	setDefaults = append(setDefaults, call)
})
if len(setDefaults) > 1 {
	for _, extra := range setDefaults[1:] {
		astutil.Report(pass, extra.Pos(), "observability-03", "slog.SetDefault called more than once in this package, configure the default logger exactly once at startup")
	}
}
```

**Exclusions:** `_test.go` files — tests routinely call `slog.SetDefault` per
test case to capture output into a buffer, which is the documented,
recommended pattern (`security-observability.md` rule 14 itself: "inject a
`*slog.Logger`... so tests can capture output"), not a startup-config
violation.

**Finding.Message:** `"slog.SetDefault called more than once in this package, configure the default logger exactly once at startup"`

**Finding.Severity:** Error. `slog.SetDefault` mutates process-wide global
state; a second call anywhere in production code is a last-writer-wins race
on every subsequent log call in the entire process, not a localized defect —
larger blast radius than a single missed trace correlation.

---

### observability-04 — EXCLUDED: log/slog operational hygiene remainder

**Source (remainder of rule 14):** "`JSONHandler` for production, `TextHandler`
for local dev, level from an env var... Inject a `*slog.Logger` through
constructors over the package-level default, so tests can capture output.
Use `slog.With` for persistent attributes and `slog.Group` for nested field
groups. Sample high-volume logs in hot paths: do not log per-request at INFO,
reserve WARN and ERROR for the rare and actionable."

**Why excluded, not encoded as a heuristic:** four independent judgment
calls, none reducible to a syntactic AST predicate at acceptable
false-positive risk:
- Handler/level selection by env var requires tracing a dataflow path from
  `os.Getenv`/`os.LookupEnv` into a `HandlerOptions.Level` field, which may
  be indirected through an arbitrary config struct — a static pass can't
  distinguish "hardcoded" from "loaded three call-frames up."
- Constructor-injected logger vs. package-level default is a type-design
  preference, not a defect; small CLIs and singleton `main` packages
  legitimately use the global logger, and distinguishing "should have
  injected" from "correctly didn't" requires knowing the type's intended
  reuse surface — a design judgment, not a pattern match.
- `slog.With`/`Group` extraction is a style optimization (deduplicating
  repeated key-value pairs across call sites); flagging "repeated attrs" as
  a violation produces high false-positive noise, since repeating a key like
  `"request_id"` across unrelated log statements is normal, not a smell.
- "Hot path" and "per-request at INFO" require call-frequency knowledge no
  static AST pass has — this is a runtime/profiling judgment.

No fixture planned for this row; it exists to document the exclusion, not to
be implemented later under a different name.

---

### observability-05 — propagate trace context across service calls

**Source (partial rule 16):** "Propagate the W3C trace context header across
service calls: that trace ID is what `slog.InfoContext` correlation actually
points at."

**Checkable:** yes, narrowed to one concrete Go idiom: an outbound HTTP
request built with `http.NewRequest` (three-arg, no context) can never carry
an OTel-instrumented trace header via context propagation — `http.NewRequestWithContext`
(or a subsequent `.WithContext(ctx)` call) is the only way the request
object is even capable of propagating a trace header downstream.

**Predicate** (`*ast.CallExpr`, walked with `insp.WithStack` to find the
enclosing assignment and block):

```go
insp.WithStack([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
	if !push {
		return true
	}
	call := n.(*ast.CallExpr)
	if !astutil.IsPkgFunc(pass, call, "net/http", "NewRequest") {
		return true
	}
	// resolve the block this call's statement lives in, and the LHS ident
	// it's assigned to via the nearest *ast.AssignStmt / *ast.BlockStmt in
	// stack — declared in this domain's Shared helpers section.
	reqName, block := assignmentContext(stack)
	if reqName != "" && block != nil && requestVarGetsWithContext(pass, block, reqName) {
		return true // req.WithContext(ctx) called later — acceptable alternative
	}
	astutil.Report(pass, call.Pos(), "observability-05", "http.NewRequest without context, use http.NewRequestWithContext to propagate trace correlation across service calls")
	return true
})
```

`assignmentContext` is declared once, in this file's Shared helpers section
above, not redefined per rule — same domain-local pattern as
`enclosingFuncDecl`.

**Exclusions:** skip when the assigned request variable is later passed
through `.WithContext(ctx)` in the same block — that's the accepted
two-step alternative to `NewRequestWithContext`, not a violation.

**Finding.Message:** `"http.NewRequest without context, use http.NewRequestWithContext to propagate trace correlation across service calls"`

**Finding.Severity:** Warning. The outbound call still executes correctly —
this is purely an observability blind spot: the downstream leg of the trace
is invisible to whoever is debugging a cross-service incident.

---

### observability-06 — EXCLUDED: span-scoping and attribute-shape judgment

**Source (remainder of rule 16):** "Create OpenTelemetry spans around
meaningful work units, not every function... Attach structured span
attributes, not free-form strings."

**Why excluded:** "meaningful work unit" is a design judgment about what
merits a span — a static pass has no notion of business significance, and
flagging "function has no span" either fires on every function (useless) or
requires a allowlist/denylist that's really a proxy for human judgment.
"Structured vs. free-form" attributes require deep knowledge of the OTel API
surface (`attribute.String` vs. a `fmt.Sprintf`-built description passed to
`AddEvent`/`SetStatus`/`RecordError` — several call shapes, each needing
separate handling) with a real risk of false positives on legitimate
formatted human-readable span descriptions. Not encoded.

(Rule 16's fourth clause — "keep PII and secrets out of span attributes" — is
Domain 3's, same redaction rule as logging; not re-listed here, see the table
at the top of this file.)

---

### observability-07 — metric label cardinality

**Source:** "Expose Prometheus counters, gauges, and histograms... Label by
stable dimensions only (route, method, status_class). Never label by user
ID, request ID, or raw error string: high-cardinality labels explode the
series count and starve the metrics backend."

**Checkable:** yes, for the literal case — a label-names slice passed as a
composite literal directly to a `promauto.New*Vec` call, containing a string
literal whose value matches a known high-cardinality substring. Not checkable
for the dynamic case (label slice built from a variable/loop) — that's a
false-negative gap, documented, not a false-positive risk, so it doesn't
disqualify the rule; it narrows its coverage.

**Predicate** (`*ast.CallExpr` → `*ast.CompositeLit` label slice → `*ast.BasicLit` elements):

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasSuffix(sel.Sel.Name, "Vec") {
		return
	}
	if !astutil.IsPkgFunc(pass, call, "github.com/prometheus/client_golang/prometheus/promauto", sel.Sel.Name) {
		return
	}
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		if _, isSlice := lit.Type.(*ast.ArrayType); !isSlice {
			continue
		}
		for _, elt := range lit.Elts {
			val, ok := astutil.StringLit(elt)
			if !ok || !matchesHighCardinalityLabel(val) {
				continue
			}
			astutil.Report(pass, elt.Pos(), "observability-07", "metric label %q has high cardinality, label by stable dimensions instead (route, method, status_class)", val)
		}
	}
})
```

**Exclusions:** only literal `[]string{...}` label-name arguments are
inspected; a label slice assembled dynamically (variable, `append`, loop)
is out of scope — a false negative, not a false positive, so it doesn't need
an exclusion guard, just a documented coverage limit.

**Finding.Message:** `"metric label %q has high cardinality, label by stable dimensions instead (route, method, status_class)"` (label value)

**Finding.Severity:** Error. This is a production-availability risk, not a
style nit — an unbounded label explodes the series count and can exhaust or
starve the metrics backend under real load, exactly the kind of silent
degradation this project's fail-loud doctrine exists to catch before it ships.

---

### observability-08 — EXCLUDED: separate liveness from readiness

**Source:** "Separate liveness from readiness. Liveness answers 'is the
process running.' Readiness answers 'can I serve traffic.' A readiness probe
checks dependencies... but must not cascade: if every pod's liveness depends
on a shared dependency, a blip kills the whole fleet."

**Why excluded, not encoded as a heuristic:** the only static signal available
is route-string keyword matching (`"/health"`, `"/live"`, `"/ready"` — no
naming standard is enforced by the language) combined with a second keyword
match inside the resolved handler body (`Ping`, `PingContext`, a client/db
method call) to detect "this handler checks a downstream dependency." Both
layers are naming conventions, not language guarantees; stacking two weak
heuristics doesn't produce one strong one, and the actual failure mode this
rule guards against — cascading fleet-wide restarts — is a Kubernetes-manifest
and call-graph concern (which probe is wired to which handler in the
Deployment spec), not something visible from a single package's AST at all.
Excluded per the recipe's explicit instruction to say so rather than invent
a fragile heuristic.

---

## 3. Fixture file spec

**Inconsistency notes, both resolved** (per `contracts.md`'s own rule: flag, don't
silently pick):

| # | Conflict | Resolution |
|---|---|---|
| 1 | `contracts.md`'s testdata layout table named this domain's dir `observability/`; this phase file's own task directive named it `audit-observability/`. | `contracts.md`'s layout table corrected to `audit-<domain>/` across all 7 Phase 4 domains, matching the convention every domain file actually used. `audit-observability/` (below) is now consistent with both. |
| 2 | `contracts.md` module section: "exactly these two [deps], nothing else." `observability-07`'s predicate requires `astutil.IsPkgFunc` to resolve `promauto.NewCounterVec` against real type info, so the fixture must import `github.com/prometheus/client_golang/prometheus{,/promauto}`. | `contracts.md`'s Dependencies section now carries an explicit fixture-only exception permitting `prometheus/client_golang` (plus the aws-sdk-go-v2 and otel packages needed by security-10/12) as a **test-only** `go.mod` requirement, never imported from `internal/` production code. |

Path: `internal/tools/testdata/fixtures/audit-observability/`. One package per
rule, per `contracts.md`'s fixture layout — never one shared
`observabilityfixture` package for the whole domain. 5 rule directories,
covering the 5 implemented rules (`observability-04/06/08` excluded — no
fixtures, per section 1/2's exclusion rationale): `rule01/`, `rule02/`,
`rule03/`, `rule05/`, `rule07/`. Each holds `violation.go` (package
`rule<NN>`, exactly one `// VIOLATION: observability-<NN>` line) and, where a
guarded near-miss exists, `compliant.go` (same package, exactly one
`// COMPLIANT: observability-<NN>` line) — except `rule03`, below, where the
near-miss must live in a `_test.go`-suffixed file for the predicate's own
exclusion to apply, so its second file is `compliant_test.go`.

`observability-03`'s predicate counts `slog.SetDefault` calls
**package-wide**, not per-file, so a naive "1 violation + 1 near-miss, same
file suffix" pair would have the near-miss's calls counted toward the
violation's total and get flagged too. Fixed by putting the near-miss in
`compliant_test.go` — `isTestFile` filters those calls out of the count
entirely (this is the exact exclusion already specified in section 2, not a
new one), so it's a legitimate near-miss, not a workaround.

### observability-01 — `rule01/`

```go
// rule01/violation.go
package rule01

import "fmt"

func LogStartupBanner() {
	// VIOLATION: observability-01
	fmt.Println("server started")
}
```

```go
// rule01/compliant.go
package rule01

import (
	"bytes"
	"fmt"
)

func WriteBuffer(buf *bytes.Buffer, count int) {
	// COMPLIANT: observability-01
	fmt.Fprintf(buf, "count=%d", count)
}
```

Near-miss reasoning: `fmt.Fprintf`'s first arg is `buf` (`*bytes.Buffer`), not `os.Stdout`/`os.Stderr` — `firstArgIsOSStream` returns false, predicate's exclusion fires, no finding. Syntactically near-identical to a real console-write.

### observability-02 — `rule02/`

```go
// rule02/violation.go
package rule02

import (
	"context"
	"log/slog"
)

func ProcessTask(ctx context.Context, id string) {
	// VIOLATION: observability-02
	slog.Info("processing task", "id", id)
}
```

```go
// rule02/compliant.go
package rule02

import "log/slog"

func StartupBanner() {
	// COMPLIANT: observability-02
	slog.Info("service starting")
}
```

Near-miss reasoning: identical `slog.Info` call shape, but `StartupBanner` has no `context.Context` parameter — `hasContextInScope` returns false, predicate's semantic guard fires, no finding.

### observability-03 — `rule03/`

```go
// rule03/violation.go
package rule03

import "log/slog"

func InitLogger() {
	slog.SetDefault(slog.Default())
}

func ReinitLogger() {
	// VIOLATION: observability-03
	slog.SetDefault(slog.Default())
}
```

```go
// rule03/compliant_test.go
package rule03

import "log/slog"

func setupTestLogger() {
	// COMPLIANT: observability-03
	slog.SetDefault(slog.Default())
}

func setupOtherTestLogger() {
	// COMPLIANT: observability-03
	slog.SetDefault(slog.Default())
}
```

Near-miss reasoning: two more `slog.SetDefault` calls, same shape as the violation — but both live in `compliant_test.go` (a `_test.go` file), so `isTestFile` drops them before they ever enter the `setDefaults` slice. Zero contribution to the package-wide count, zero findings, and critically: does not pollute `violation.go`'s count either (that file alone has 2 calls, `InitLogger`'s is first/unflagged, `ReinitLogger`'s is the sole flagged extra).

### observability-05 — `rule05/`

```go
// rule05/violation.go
package rule05

import (
	"context"
	"net/http"
)

func FetchDownstream(ctx context.Context, url string) (*http.Response, error) {
	// VIOLATION: observability-05
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}
```

```go
// rule05/compliant.go
package rule05

import (
	"context"
	"net/http"
)

func FetchDownstreamWithContext(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// COMPLIANT: observability-05
	req = req.WithContext(ctx)
	return http.DefaultClient.Do(req)
}
```

Near-miss reasoning: the exact same `http.NewRequest("GET", url, nil)` call shape as the violation — but `req` is later fed through `req.WithContext(ctx)` in the same block. `requestVarGetsWithContext` returns true, predicate's exclusion fires, no finding.

### observability-07 — `rule07/`

```go
// rule07/violation.go
package rule07

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var requestsByUser = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "requests_total",
}, []string{
	"route",
	// VIOLATION: observability-07
	"user_id",
})
```

```go
// rule07/compliant.go
package rule07

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var requestsByRoute = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "requests_stable_total",
}, []string{
	"route",
	"method",
	// COMPLIANT: observability-07
	"status_class",
})
```

Near-miss reasoning: same call shape (`promauto.NewCounterVec` with a literal `[]string` label slice) — but `"status_class"` matches none of `highCardinalityLabelSubstrings`, so `matchesHighCardinalityLabel` returns false and that element is skipped.

---

## 4. Tool file spec

Three parts, per `contracts.md`'s required §4 structure: the analysis
subpackage (this domain's `*analysis.Analyzer`), the orchestration pointer
(project-wide, not restated per domain), and the tool file (MCP-facing
handler). The Shared-helpers section above (this file's own package
preamble, `init()`, and helper set) and section 2's 5 predicates are all part
of the same `observability.go` file described below — not repeated here.

### Analysis subpackage — `internal/analysis/observability/observability.go`

```go
package observability

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "observability",
	Doc:      "flags logging/tracing/metrics practices that degrade incident debuggability (rules observability-01/02/03/05/07)",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// section 2's 5 predicates run against insp here, each ending in a
	// call to astutil.Report — no return value beyond (nil, nil), findings
	// are collected by astutil.Report into the pass's shared result sink.
	return nil, nil
}
```

Only `Analyzer` is exported, per `contracts.md`'s Naming section — `run`,
`init()`, and every helper in the Shared-helpers section above stay
unexported.

### Orchestration

Not domain-specific. `internal/audit.Run(ctx, ws, pattern, analyzers)` is the
single project-wide orchestration call site — one `packages.Load` (the full
`Need*` bit set including `NeedTypesSizes`, plus `Tests: true`), one
`dedupeTestVariants(pkgs)` pass, one `checker.Analyze(analyzers, pkgs, opts)`
call, one `collect(graph, ws, pkgs)` into `finding.AuditResult` — specified
exactly once in `contracts.md`'s Orchestration section. This domain
contributes `observability.Analyzer` to that one call site; it does not
implement its own `packages.Load` or its own pass-running loop.

### Tool file — `internal/tools/go_audit_observability.go`

```go
package tools

import (
	"context"
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/observability"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditObservabilityInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditObservabilityOutput struct {
	Result finding.AuditResult `json:"result"`
}

func AuditObservabilityHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditObservabilityInput) (*mcp.CallToolResult, AuditObservabilityOutput, error) {
	if err := normalizeAuditObservabilityInput(&in); err != nil {
		return nil, AuditObservabilityOutput{}, fmt.Errorf("validating input: %w", err)
	}
	ws, err := resolveInWorkspace(in.Package)
	if err != nil {
		return nil, AuditObservabilityOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{observability.Analyzer})
	if err != nil {
		return nil, AuditObservabilityOutput{}, fmt.Errorf("running observability audit: %w", err)
	}
	return nil, AuditObservabilityOutput{Result: result}, nil
}

func normalizeAuditObservabilityInput(in *AuditObservabilityInput) error {
	if in.MaxFindings == 0 {
		in.MaxFindings = 200
	}
	if in.MaxFindings > 1000 {
		in.MaxFindings = 1000
	}
	if in.MinSeverity == "" {
		in.MinSeverity = finding.SeverityInfo
	}
	return nil // extend per-domain only if a field needs real validation beyond defaulting
}
```

`RegisterAuditObservability` (tool registration, annotations verbatim from
`contracts.md`'s audit-tool-family default — no per-domain override):

```go
func RegisterAuditObservability(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_audit_observability",
		Description: "Audits a Go package for logging/tracing/metrics practices that degrade incident debuggability",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, AuditObservabilityHandler)
}
```

`Package` carries no `jsonschema:"...,required"` fragment — required is
expressed by the field's absence of `,omitempty`, never by a schema-tag
fragment (`contracts.md`'s jsonschema-tag rule; the previous draft of
this section had `,required` on `Package`, now corrected). The `Need*` bit
list (including the `NeedTypesSizes` bit `performance-02` depends on) lives
once, centrally, in `internal/audit/run.go` — not duplicated in this file or
any other domain's tool file.

---

## 5. Verification spec

`internal/analysis/observability/observability_test.go`, package
`observability_test`. Exercises `observability.Analyzer` directly via
`astutil.RunFixture`, per `contracts.md`'s literal `TestAudit<Domain>_*`
template — no `packages.Load`/handler round-trip in this test, that
plumbing is `internal/audit`'s job and is verified once, centrally, not
per-domain. `Location.File` is always the real workspace-relative path
(`filepath.ToSlash`, never `filepath.Base`) — every assertion below checks
the full path, not just a line number.

```go
package observability_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/observability"
)

func TestAuditObservability_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule01")
	got := astutil.FindingsForRule(findings, "observability-01")
	require.Len(t, got, 1)
	assert.Equal(t, "fixtures/audit-observability/rule01/violation.go", got[0].Location.File)
	assert.Equal(t, 7, got[0].Location.Line)
}

func TestAuditObservability_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule01")
	for _, f := range findings {
		assert.NotEqual(t, "observability-01", f.Rule, "compliant.go's Fprintf to a non-os stream must not be flagged")
	}
}

func TestAuditObservability_Rule02(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule02")
	got := astutil.FindingsForRule(findings, "observability-02")
	require.Len(t, got, 1)
	assert.Equal(t, "fixtures/audit-observability/rule02/violation.go", got[0].Location.File)
	assert.Equal(t, 10, got[0].Location.Line)
}

func TestAuditObservability_Rule02_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule02")
	for _, f := range findings {
		assert.NotEqual(t, "observability-02", f.Rule, "compliant.go's slog.Info with no context in scope must not be flagged")
	}
}

func TestAuditObservability_Rule03(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule03")
	got := astutil.FindingsForRule(findings, "observability-03")
	require.Len(t, got, 1)
	assert.Equal(t, "fixtures/audit-observability/rule03/violation.go", got[0].Location.File)
	assert.Equal(t, 11, got[0].Location.Line) // ReinitLogger's call — the second, "extra" slog.SetDefault; InitLogger's first call at line 6 is never flagged
}

func TestAuditObservability_Rule03_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule03")
	for _, f := range findings {
		assert.NotEqual(t, "observability-03", f.Rule, "compliant_test.go's two slog.SetDefault calls live in a _test.go file and must not be flagged")
	}
}

func TestAuditObservability_Rule05(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule05")
	got := astutil.FindingsForRule(findings, "observability-05")
	require.Len(t, got, 1)
	assert.Equal(t, "fixtures/audit-observability/rule05/violation.go", got[0].Location.File)
	assert.Equal(t, 10, got[0].Location.Line)
}

func TestAuditObservability_Rule05_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule05")
	for _, f := range findings {
		assert.NotEqual(t, "observability-05", f.Rule, "compliant.go's http.NewRequest is followed by req.WithContext(ctx) and must not be flagged")
	}
}
func TestAuditObservability_Rule07(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule07")
	got := astutil.FindingsForRule(findings, "observability-07")
	require.Len(t, got, 1)
	assert.Equal(t, "fixtures/audit-observability/rule07/violation.go", got[0].Location.File)
	assert.Equal(t, 13, got[0].Location.Line)
}

func TestAuditObservability_Rule07_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, observability.Analyzer, "audit-observability/rule07")
	for _, f := range findings {
		assert.NotEqual(t, "observability-07", f.Rule, `compliant.go's "status_class" label matches no high-cardinality substring and must not be flagged`)
	}
}

func TestAuditObservability_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("observability"), 5)
}
```

Each `Rule<NN>` test's line number is the exact line of the `// VIOLATION:`-
adjacent statement in that rule's `violation.go`, per the fixture content in
section 3 above — not a restated description. The `CompliantIsSilent` tests
re-run the same fixture directory and assert none of its returned findings
carry that rule's ID; as written this can only fail if the true-positive
test's own `require.Len(t, got, 1)` above it would already have failed
(`observability`'s `violation.go` and `compliant.go` are compiled as one
package per rule, so both files' findings are in the same `findings` slice)
— this redundancy is inherited verbatim from `contracts.md`'s own
canonical `CompliantIsSilent` template, not introduced here; see this file's
final report for the cross-domain note.
