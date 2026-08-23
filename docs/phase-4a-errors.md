# Domain 2 — Errors (`go_audit_errors`)

> **Release status:** included in v0.1.0.

The original 18-rule error-handling corpus is reproduced in this specification;
no external reference file is required.
Extends `Finding`/`AuditResult`/`Severity`/`Location` and the pass skeleton from `contracts.md`. Does not redefine them.

## Shared helpers

Domain-specific helpers below live in `internal/analysis/errors/errors.go`, `package errors` —
this domain's own analysis subpackage (`contracts.md`'s Naming and file layout: one
subpackage per domain, package name matching the directory, never `package analysis` — that name
self-collides with the `golang.org/x/tools/go/analysis` import). This domain no longer declares
`stringLitValue`, `isQualifiedCall`, or `resolvedTypeString`: all three's duplicated logic is now
centralized in `internal/analysis/astutil/`, and every call site below and in section 2 uses
`astutil.StringLit`, `astutil.IsPkgFunc`, and `astutil.TypeString` instead (`resolvedTypeString`'s
body was byte-identical to `phase-4a-concurrency.md`'s `typeString` — the same duplicate-helper defect,
resolved the same way). The remaining helpers are genuinely errors-domain logic with no astutil
equivalent (no other domain needs an `error`-interface type check, an `<ident> != nil` matcher, or
an `errors.New`/`fmt.Errorf` argument extractor) and stay declared here.

```go
// isErrorType reports whether e's static type is exactly the `error` interface.
func isErrorType(pass *analysis.Pass, e ast.Expr) bool {
	return astutil.TypeString(pass, e) == "error"
}

// errCheckIdent matches `<ident> != nil` and returns the ident.
func errCheckIdent(cond ast.Expr) (*ast.Ident, bool) {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return nil, false
	}
	id, ok := be.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	nilIdent, ok := be.Y.(*ast.Ident)
	if !ok || nilIdent.Name != "nil" {
		return nil, false
	}
	return id, true
}

// exprRefersToIdent reports whether name appears anywhere inside e (covers
// `return err` and `return fmt.Errorf("...: %w", err)` alike).
func exprRefersToIdent(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return true
	})
	return found
}

// isFmtErrorfOrNew returns the format/message argument of fmt.Errorf or
// errors.New, and which one matched.
func isFmtErrorfOrNew(pass *analysis.Pass, call *ast.CallExpr) (arg ast.Expr, isErrorf, ok bool) {
	if astutil.IsPkgFunc(pass, call, "fmt", "Errorf") && len(call.Args) >= 1 {
		return call.Args[0], true, true
	}
	if astutil.IsPkgFunc(pass, call, "errors", "New") && len(call.Args) >= 1 {
		return call.Args[0], false, true
	}
	return nil, false, false
}

// enclosingFuncDecl walks the ancestor stack (via insp.WithStack, same
// technique as concurrency-18's inLoop tracking) and returns the nearest
// *ast.FuncDecl, or nil if the node sits at package scope (e.g. a top-level
// var initializer).
func enclosingFuncDecl(stack []ast.Node) *ast.FuncDecl {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}
```

Rule-specific helpers are shown inline in each rule's predicate section below (they're only used
by that one rule and adding them here would force readers to cross-reference).

---

## 1. Rules table

| Rule ID | Source rule (paraphrased) | Status | Severity |
|---|---|---|---|
| `errors-01` | error is always the last return value | **Disabled (v0.1.0; calibrated)** | — |
| `errors-02` | exported functions return `error`, never a concrete type | **Disabled (v0.1.0; calibrated)** | — |
| `errors-03` | happy path unindented after `if err != nil`, not nested in `else` | **Disabled (v0.1.0; calibrated)** | — |
| `errors-04` | handle each error exactly once: log OR return, never both | Implemented | Error |
| `errors-05` | wrap errors with context at each call boundary | **Disabled (v0.1.0; calibrated)** | — |
| `errors-06` | noun phrases for context, not "failed to" prefixes | **Disabled (v0.1.0; calibrated)** | — |
| `errors-07` | error strings: lowercase, no trailing punctuation, no capitalized acronyms | **Disabled (v0.1.0; calibrated)** | — |
| `errors-08` | use `%w` only when callers need `errors.Is`/`As`; `%v` to hide internals | **Excluded** | — |
| `errors-09` | never discard an error with `_` without an explanatory comment | Implemented | Error |
| `errors-10` | no `panic` in library code | **Disabled (v0.1.0; calibrated)** | — |
| `errors-11` | `MustXYZ` only at startup/package-init, never in request/worker paths | **Disabled (v0.1.0; calibrated)** | — |
| `errors-12` | translate errors to canonical codes at system boundaries, not by string match | **Disabled (v0.1.0; calibrated)** | — |
| `errors-13` | `errors.Join` for parallel failures; don't fake a linear chain with multiple `%w` | **Disabled (v0.1.0; calibrated)** | — |
| `errors-14` | custom wrapper types implement `Unwrap`/`Is`/`As` as needed by callers | **Disabled (v0.1.0; calibrated)** | — |
| `errors-15` | recover panics at goroutine/request boundaries | **Disabled (v0.1.0; calibrated)** | — |
| `errors-16` | capture deferred close errors with named returns | **Disabled (v0.1.0; calibrated)** | — |
| `errors-17` | classify retryable vs. terminal errors with data, not string matching | **Disabled (v0.1.0; calibrated)** | — |
| `errors-18` | label error metrics by a stable code bucket, not the raw error string | **Excluded** | — |
| `errors-19` | `os.Exit`/`log.Fatal*` outside `main` | Implemented | Error |

3 active, 14 disabled after external calibration, 2 excluded. Disabled rules
retain their predicates and detailed fixture/design material below for future
redesign, but emit no findings and are excluded from the active rule resource.
Rule numbering preserves the source file's own 1–18 ordering
(`errors-01` through `errors-18`); gaps at 08 and 18 are intentional, not renumbered.
`errors-19` is a new rule appended beyond the source file's 18 — sourced from this project's
documented Go convention (not `error-handling.md`), continuing the sequential ID
space rather than reusing either gap.

---

## 2. Per-rule AST pattern

<a id="errors-01"></a>
### errors-01 — error last return value

**v0.1.0 status:** Disabled after external calibration. Non-final error results
were intentional API ordering, including APIs with more than one error result;
the convention is not a correctness predicate. The retained predicate emits
no finding.

**Source:** "`error` is always the last return value. A function returning `(User, error)` is
idiomatic; `(error, User)` is not."

**Why checkable:** Return-parameter order is pure syntax — no semantic judgment. A field typed
exactly `error` (via `go/types`, not the identifier text `error`) that isn't the last entry in the
result list is unconditionally the antipattern; there's no legitimate reason to put it elsewhere.

**Node type(s):** `*ast.FuncDecl`, `*ast.FuncLit` → `.Type.Results` (`*ast.FieldList`).

```go
func errorNotLastReturn(pass *analysis.Pass, results *ast.FieldList) (bad *ast.Field, ok bool) {
	if results == nil {
		return nil, false
	}
	n := len(results.List)
	for i, f := range results.List {
		if astutil.TypeString(pass, f.Type) != "error" {
			continue
		}
		if i != n-1 {
			return f, true
		}
	}
	return nil, false
}
```

**Exclusions:** A grouped field `(a, b error)` is one `*ast.Field` entry with two names — if that
entry is last, it's compliant regardless of name count. Blank/unnamed result lists in interface
method signatures are covered identically (same `FieldList` shape).

**Finding.Message:** `"function %s returns error at position %d of %d results, not last — move error to the final return value"`

**Finding.Severity:** `SeverityWarning` — wrong-but-working; every caller must destructure
non-idiomatically, but it doesn't corrupt data or crash. Not `Error` because it compiles and
functions correctly; not `Info` because it breaks every caller's `if err != nil` muscle memory.

---

<a id="errors-02"></a>
### errors-02 — exported concrete error type

**v0.1.0 status:** Disabled after external calibration. The predicate and
fixture below are retained as future redesign documentation; this rule emits
no findings and is not part of the active rule resource.

**Source:** "Exported functions return the `error` interface, never a concrete error type.
Concrete types create accidental API contracts and couple callers to your implementation."

**Why checkable:** `go/types.Implements` gives an exact, non-judgmental answer to "does this type
satisfy `error`." Combined with an exported function name (capitalized first letter) and a result
field whose type is a named type implementing `error` but is not itself the `error` interface,
this is fully mechanical.

**Node type(s):** `*ast.FuncDecl` (exported, `Recv == nil` or exported method), `.Type.Results`.

```go
func isConcreteErrorType(pass *analysis.Pass, e ast.Expr) (typeName string, ok bool) {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil || t.String() == "error" {
		return "", false
	}
	errIface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if types.Implements(t, errIface) || types.Implements(types.NewPointer(t), errIface) {
		return t.String(), true
	}
	return "", false
}

func exportedConcreteErrorReturn(pass *analysis.Pass, fn *ast.FuncDecl) (field *ast.Field, typeName string, ok bool) {
	if !fn.Name.IsExported() || fn.Type.Results == nil {
		return nil, "", false
	}
	for _, f := range fn.Type.Results.List {
		if name, matched := isConcreteErrorType(pass, f.Type); matched {
			return f, name, true
		}
	}
	return nil, "", false
}
```

**Exclusions:** Unexported functions are out of scope — an internal helper returning a concrete
error type is fine because callers are all in the same package and already coupled. A result type
that's an interface embedding `error` (not a concrete struct/pointer) doesn't match
`isConcreteErrorType` because `Implements` still holds but the type IS an interface — add an
explicit `types.IsInterface(t)` guard: skip if `t.Underlying()` is `*types.Interface`.

**Finding.Message:** `"exported function %s returns concrete error type %s — return the error interface and export %s for errors.As"`

**Finding.Severity:** `SeverityError` — this is a compile-time-visible API contract that every
downstream module (four owned services import each other's client packages) locks in; changing it
later is a breaking change across service boundaries, not a local cleanup.

---

<a id="errors-03"></a>
### errors-03 — happy path in `else`

**Source:** "Handle errors before the happy path. Put the error path in the `if`, leave the happy
path unindented after."

**Why checkable:** Purely structural: does the `if err != nil { ... }` have a non-nil,
non-`else-if` `Else` block. No semantic judgment needed — an `else` clause containing the
continuation logic is definitionally the nested-happy-path shape regardless of what that logic is.

**Node type(s):** `*ast.IfStmt` (`Cond` matches `errCheckIdent` + `isErrorType`), `.Else`.

```go
func isHappyPathInElse(ifStmt *ast.IfStmt) bool {
	if ifStmt.Else == nil {
		return false
	}
	_, isElseIf := ifStmt.Else.(*ast.IfStmt)
	return !isElseIf // else-if chains a second condition; that's dispatch, not nested happy path
}
```

**Exclusions:** `else if` (checking a second, different error or condition) is excluded — that's
legitimate multi-branch dispatch, not the happy-path-buried antipattern.

**Finding.Message:** `"error check at %s nests the happy path inside an else block — leave the happy path unindented after the if"`

**Finding.Severity:** `SeverityInfo` — pure readability/style, doesn't change behavior or
correctness.

---

<a id="errors-04"></a>
### errors-04 — log AND return (flagship rule)

**Source:** "Handle each error exactly once: log OR return, never both. Logging then returning
causes duplicate noise and makes incident responders see the same failure at two stack depths."

**Why checkable:** Fully mechanical given a fixed whitelist of logging-method selector names: an
`if err != nil` block containing (a) a call whose selector name is in the logging whitelist AND
whose arguments reference the same `err` identifier, followed later in the same block by (b) a
`return` statement whose results also reference that identifier.

**Node type(s):** `*ast.IfStmt`, `.Body.List` → `*ast.ExprStmt`/`*ast.CallExpr` for the log call,
`*ast.ReturnStmt` for the return.

```go
var loggingMethodNames = map[string]bool{
	"Error": true, "Errorf": true, "Errorln": true, "ErrorContext": true,
	"Warn": true, "Warnf": true, "WarnContext": true, "Err": true,
}

func isLoggingCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && loggingMethodNames[sel.Sel.Name]
}

func callArgsReferenceIdent(call *ast.CallExpr, name string) bool {
	for _, a := range call.Args {
		if exprRefersToIdent(a, name) {
			return true
		}
	}
	return false
}

// isLogThenReturn scans block for a logging call over errName followed by a
// return that also references errName, in that order.
func isLogThenReturn(block *ast.BlockStmt, errName string) (logStmt, retStmt ast.Stmt, ok bool) {
	for i, stmt := range block.List {
		es, isExpr := stmt.(*ast.ExprStmt)
		if !isExpr {
			continue
		}
		call, isCall := es.X.(*ast.CallExpr)
		if !isCall || !isLoggingCall(call) || !callArgsReferenceIdent(call, errName) {
			continue
		}
		for _, later := range block.List[i+1:] {
			ret, isRet := later.(*ast.ReturnStmt)
			if !isRet {
				continue
			}
			for _, r := range ret.Results {
				if exprRefersToIdent(r, errName) {
					return stmt, later, true
				}
			}
		}
	}
	return nil, nil, false
}
```

**Exclusions:** The logging-method whitelist is closed (`Error`/`Errorf`/`Errorln`/`ErrorContext`/
`Warn`/`Warnf`/`WarnContext`/`Err`) — a call to an unrelated method that happens to be named
`Error()` on a non-logger receiver (e.g. the `error` interface's own `Error() string`) never has
side-effect-shaped args referencing `err` as a logging field, so false positives are structurally
unlikely; this is a documented, accepted narrowing (closed whitelist over open-ended "looks like a
logger" inference). `_test.go` and package `main` are excluded after calibration found test assertions
and terminal CLI diagnostics where returning after logging is intentional.

**Finding.Message:** `"err is logged via %s and then returned at %s — pick one: log, or wrap and return, never both"`

**Finding.Severity:** `SeverityError` — this is the reference doc's own flagship "most common
Claude mistake," with a real production incident cited (Production Note: staging alert storm,
one timeout misread as dozens of failures). Direct incident-response cost, not style.

---

<a id="errors-05"></a>
### errors-05 — bare `return err` with no context

**Source:** "Wrap errors with context at each meaningful call boundary... Without context, a bare
`connection refused` tells you nothing about which downstream died."

**Why checkable:** An `if err != nil { return err }` block where the return's sole result is the
bare identifier (not wrapped in `fmt.Errorf`, not a different named error) is syntactically exact.

**Node type(s):** `*ast.IfStmt`, `.Body.List` → `*ast.ReturnStmt`, `.Results[i]` → `*ast.Ident`.

```go
func isBareReturnErr(ret *ast.ReturnStmt, errName string) bool {
	for _, r := range ret.Results {
		if id, ok := r.(*ast.Ident); ok && id.Name == errName {
			return true
		}
	}
	return false
}
```

**Exclusions:** If the same `if`-block also matches `errors-04`'s `isLogThenReturn` predicate,
skip — that block is reported once, under `errors-04`, which is the more specific and more severe
finding for the exact same statement. Without this exclusion the same `return err` line would be
double-counted under two rule IDs.

**Finding.Message:** `"err returned bare with no context at %s — wrap at this boundary: fmt.Errorf(\"...: %%w\", err)"`

**Finding.Severity:** `SeverityWarning` — loses debuggability (which downstream failed) but
doesn't corrupt behavior; distinct from `errors-04` which is the double-handling failure mode.

---

<a id="errors-06"></a>
### errors-06 — "failed to" prefix

**Source:** "Noun phrases for context, not 'failed to' prefixes... Noun phrases stack cleanly."

**Why checkable:** A `fmt.Errorf`/`errors.New` call whose literal format string starts with the
case-insensitive substring `"failed to"` is an exact string-prefix check on a compile-time
constant — no dynamic string, no ambiguity.

**Node type(s):** `*ast.CallExpr` (`isFmtErrorfOrNew`), `.Args[0]` → `*ast.BasicLit`.

```go
func hasFailedToPrefix(s string) bool {
	return strings.HasPrefix(strings.ToLower(s), "failed to")
}
```

**Exclusions:** Only applies when the format string is a literal (`astutil.StringLit` succeeds). A
dynamically-built format string (`fmt.Sprintf(...)` passed as the format arg) is unresolvable at
compile time — skip rather than guess.

**Finding.Message:** `"error message at %s starts with a \"failed to\" prefix — use a noun phrase instead (e.g. \"fetching user %%s: %%w\")"`

**Finding.Severity:** `SeverityInfo` — pure wording/style, zero functional impact.

---

<a id="errors-07"></a>
### errors-07 — string casing/punctuation/acronyms

**Source:** "Error strings: lowercase, no ending punctuation, no capitalized acronyms.
`\"invalid message id\"` not `\"Invalid message ID.\"`."

**Why checkable:** Three independent, exact syntactic checks on a literal string constant: first
rune uppercase, last rune in `.!?`, or a `[A-Z]{2,}` run anywhere in the string. All decidable from
the literal text alone.

**Node type(s):** `*ast.CallExpr` (`isFmtErrorfOrNew`), `.Args[0]` → `*ast.BasicLit`.

```go
var acronymRe = regexp.MustCompile(`[A-Z]{2,}`)

func stringStyleViolation(s string) (reason string, bad bool) {
	if s == "" {
		return "", false
	}
	r := []rune(s)
	switch {
	case unicode.IsUpper(r[0]):
		return "starts with an uppercase letter", true
	case r[len(r)-1] == '.' || r[len(r)-1] == '!' || r[len(r)-1] == '?':
		return "ends with punctuation", true
	case acronymRe.MatchString(s):
		return "contains a capitalized acronym", true
	default:
		return "", false
	}
}
```

**Exclusions:** Literal-only, same as `errors-06`. A leading `%s`/`%d` verb (dynamic value first)
means the literal itself may legitimately start lowercase-adjacent to a verb — the check still
applies to the literal text as written since Go verbs are lowercase (`%s`, `%d`, `%w`), so no
special-case is needed.

**Finding.Message:** `"error string at %s %s — use lowercase, no trailing punctuation, no capitalized acronyms"` (reason interpolated from `stringStyleViolation`)

**Finding.Severity:** `SeverityInfo` — style only; doesn't affect `errors.Is`/`As` chains or
runtime behavior.

---

<a id="errors-08"></a>
### errors-08 — EXCLUDED: `%w` vs `%v`

**Source:** "Use `%w` only when callers need `errors.Is`/`errors.As` on the wrapped type. Use `%v`
to hide internals from consumers."

**Why excluded:** The correct choice depends entirely on caller intent — "do external callers need
to `errors.Is`/`As` through this boundary" — which is a design decision about the package's public
contract, not a syntactic or type-level property of the call site itself. A blanket rule ("flag
every `%w` in an exported function" or "flag every `%w` crossing a package boundary") would
misfire constantly: many internal-to-internal wraps legitimately want `%w` for `errors.Is` chains
within the same module (e.g. a dispatcher wrapping a reader's sentinel errors is often
intentional), and the reference doc's own "CORRECT" example in Trap #2 uses `%w`-free `%v`
specifically at a repository boundary, not universally. No AST/types predicate distinguishes
"caller needs Is/As here" from "caller doesn't" without knowing the call graph's external
consumers, which is out of scope for a single-package `go/analysis` pass. Excluded rather than
approximated with a boundary-guessing heuristic.

---

<a id="errors-09"></a>
### errors-09 — bare error discard without comment

**Source:** "Never discard an error with `_` without a comment explaining why it is safe.
`_, err := io.Copy(dst, src); _ = err` is a bug."

**Why checkable:** `_ = err` is an exact `*ast.AssignStmt` shape (single blank LHS, single
error-typed RHS identifier). Presence/absence of an attached comment is a direct `ast.CommentMap`
lookup — no interpretation of the comment's content required, only its presence.

**Node type(s):** `*ast.AssignStmt` (`Tok == token.ASSIGN`), `.Lhs[0]`/`.Rhs[0]` → `*ast.Ident`.

```go
func isBareErrDiscard(pass *analysis.Pass, assign *ast.AssignStmt) bool {
	if assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	blank, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || blank.Name != "_" {
		return false
	}
	id, ok := assign.Rhs[0].(*ast.Ident)
	return ok && isErrorType(pass, id)
}
// flagged iff isBareErrDiscard(pass, assign) && len(cmap[assign]) == 0
```

`cmap` is the standard `ast.NewCommentMap(pass.Fset, file, file.Comments)` built once per file in
the pass's `run` function (same technique contracts.md's skeleton uses for any comment-aware rule).

**Exclusions:** `_, _ = f(...)` where the discarded value isn't error-typed doesn't match (the
`isErrorType` guard). Any attached comment — regardless of wording — satisfies the rule; this
tool checks presence, not content quality (content quality is a human-review judgment call, out of
scope).

**Finding.Message:** `"error discarded with \"_ = %s\" at %s with no comment explaining why it's safe"`

**Finding.Severity:** `SeverityError` — a silently dropped error on an untrusted input or routing
path is a data-loss risk, not a style nit; matches
`go-security.md`'s "zero tolerance for silently ignored errors."

---

<a id="errors-10"></a>
### errors-10 — `panic` in library code

**Source:** "No `panic` in library code. `panic` is for unrecoverable startup failures in `main`,
not control flow."

**Why checkable:** `panic` is a predeclared identifier (not a keyword), so `go/types` can
distinguish a genuine builtin call from a locally-shadowed identifier of the same name via
`pass.TypesInfo.Uses[id]`'s object kind. Combined with an enclosing-function check
(`main`/`init` in `package main`), this is fully mechanical.

**Node type(s):** `*ast.CallExpr` (`Fun` is `*ast.Ident` named `"panic"`), enclosing
`*ast.FuncDecl` via `enclosingFuncDecl`.

```go
func isPanicCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	_, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin)
	return isBuiltin
}

func isStartupContext(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn == nil || pass.Pkg.Name() != "main" {
		return false
	}
	return fn.Name.Name == "main" || fn.Name.Name == "init"
}
```

**Exclusions:** `isPanicCall`'s `*types.Builtin` check guards against a locally-shadowed `panic`
(e.g. a local var or func literal named `panic` — legal in Go since it's not a keyword); such a
call resolves to a `*types.Var`/`*types.Func`, not `*types.Builtin`, and is correctly excluded. The
startup-context exclusion (`main`/`init` inside `package main`) cannot be exercised inside the
`rule10` testdata package itself since that package is never named `main` — see the Fixture spec
note under `errors-10` for how this gap is covered instead.

**Finding.Message:** `"panic(...) at %s outside main/init — return an error instead; panic is not control flow in library code"`

**Finding.Severity:** `SeverityError` — an unrecovered panic while processing one malformed
record can take down the entire ingestion worker; matches
`go-security.md`'s recover-boundary requirement.

---

<a id="errors-11"></a>
### errors-11 — `MustXYZ` outside startup

**Source:** "`MustXYZ` helpers only at program startup or package-level init. Never in request
handlers or workers."

**Why checkable:** The `Must` prefix is a naming convention the codebase itself commits to (per
`error-handling.md` rule 14's sentinel/typed/opaque taxonomy), so detecting the identifier prefix
is exact. Whether the call site is a startup context reduces to checking the enclosing
`*ast.FuncDecl`'s name (or its absence, for package-level `var` initializers).

**Node type(s):** `*ast.CallExpr` (`Fun` is `*ast.Ident` or `*ast.SelectorExpr` with a `Must`-
prefixed name), enclosing `*ast.FuncDecl` via `enclosingFuncDecl` (nil at package scope).

```go
func isMustCall(call *ast.CallExpr) (name string, ok bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if strings.HasPrefix(fn.Name, "Must") {
			return fn.Name, true
		}
	case *ast.SelectorExpr:
		if strings.HasPrefix(fn.Sel.Name, "Must") {
			return fn.Sel.Name, true
		}
	}
	return "", false
}

func isAllowedMustCallSite(fn *ast.FuncDecl) bool {
	if fn == nil {
		return true // package-level var/const initializer — canonical startup site
	}
	switch fn.Name.Name {
	case "main", "init", "TestMain":
		return true
	}
	return strings.HasPrefix(fn.Name.Name, "Must") // Must-chain delegating to another Must*
}
```

**Exclusions:** A `Must*` helper calling another `Must*` helper (delegation chain) is allowed —
`isAllowedMustCallSite`'s last branch. Test files (`_test.go`, excluding `TestMain`) are NOT
special-cased here; `TestMain` specifically is allowed since it's itself a startup hook run once
per test binary, but ordinary `Test*`/`Benchmark*` functions calling `Must*` setup helpers are
intentionally still flagged — narrowed scope, documented: if the codebase wants `Must*` freely in
table-driven test setup, that's a separate carve-out this rule does not make automatically.

**Finding.Message:** `"%s called at %s outside main/init/TestMain/package-init — Must* helpers panic and must not run in request or worker paths"`

**Finding.Severity:** `SeverityError` — a `Must*` panic firing inside a live worker takes down
the goroutine handling that work item (or the process, if unrecovered) mid-flight.

---

<a id="errors-12"></a>
### errors-12 — boundary translation by string match

**Source:** "At system boundaries (HTTP, gRPC, SQS), translate errors into canonical codes... A raw
`sql.ErrNoRows` surfacing as a gRPC `Internal` is an implementation leak."

**Why checkable:** `strings.Contains(<expr>.Error(), <literal>)` is an exact call-expression shape:
outer call is `strings.Contains` (resolved via `go/types`, not text), first argument is a call to
`.Error()` on an error-typed receiver, second argument is a string literal. This is the doc's own
"WRONG" exemplar verbatim.

**Node type(s):** `*ast.CallExpr` (`strings.Contains`), nested `*ast.CallExpr` (`.Error()`),
`*ast.SelectorExpr`.

```go
func isErrorStringMatchCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !astutil.IsPkgFunc(pass, call, "strings", "Contains") || len(call.Args) != 2 {
		return false
	}
	inner, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" || len(inner.Args) != 0 {
		return false
	}
	return isErrorType(pass, sel.X)
}
```

`errors-12` and `errors-17` share this exact predicate (the doc's Trap #7 and rule 17 use the
identical `strings.Contains(err.Error(), ...)` shape for two different purposes — status-code
mapping vs. retry classification). They're disambiguated by the enclosing function's result shape,
not by the call site itself:

```go
// classifyStringMatchRule: a single-bool-result function is doing a
// retry/terminal classification (errors-17); anything else (int status code,
// http.ResponseWriter side effect, multi-result, etc.) is boundary-code
// mapping (errors-12). Documented narrowing — the two rules are the same
// AST pattern read through two different call-site intents.
func classifyStringMatchRule(fn *ast.FuncDecl) string {
	if fn == nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return "errors-12"
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && id.Name == "bool" {
		return "errors-17"
	}
	return "errors-12"
}
```

**Exclusions:** Only `strings.Contains` is matched — `strings.HasPrefix`/`==` against
`.Error()` are equally bad in principle but the source doc's own exemplar is specifically
`Contains`; extending to every string-comparison function raises false-positive risk against
legitimate non-error string logic without a matching source citation, so scope is held to the
documented shape.

**Finding.Message:** `"error classified by strings.Contains(err.Error(), ...) at %s — use errors.Is/As against a sentinel or typed error for canonical code mapping"`

**Finding.Severity:** `SeverityWarning` — breaks the moment error wording changes or the error
gets wrapped one more layer; a real production bug class per the doc, but not immediately-crashing
severity like `errors-09`/`errors-15`.

---

<a id="errors-13"></a>
### errors-13 — multiple `%w` verbs faking a linear chain

**v0.1.0 status:** Disabled after external calibration. Go supports multiple
`%w` verbs in one `fmt.Errorf` call, and the corpus contained legitimate
multi-cause wrapping. The original diagnostic was factually wrong and emits
no finding.

**Source:** "`errors.Join`... folds independent errors into one... a single `fmt.Errorf` may carry
multiple `%w` verbs. Use `Join` for parallel failures, not to fake a linear chain."

**Why checkable:** Counting `%w` occurrences in a literal format string passed to `fmt.Errorf` is
an exact string operation — no ambiguity about what "multiple" means (`> 1`).

**Node type(s):** `*ast.CallExpr` (`fmt.Errorf` only, via `isFmtErrorfOrNew`'s `isErrorf` flag),
`.Args[0]` → `*ast.BasicLit`.

```go
func hasMultipleWrapVerbs(format string) bool {
	return strings.Count(format, "%w") > 1
}
```

**Exclusions:** `errors.New` is excluded (`isErrorf` must be true) — `%w` is meaningless without
`Errorf`. Literal-only, same as `errors-06`/`errors-07`.

**Finding.Message:** `"fmt.Errorf at %s carries %d %%w verbs — %%w wraps one cause per call; use errors.Join for independent parallel failures"`

**Finding.Severity:** `SeverityWarning` — `errors.Is`/`As` still traverse all joined causes per
the doc (rule 13), so this isn't silently broken, but it's semantically wrong (implies a false
causal chain) and confuses incident responders reading the message.

---

<a id="errors-14"></a>
### errors-14 — custom wrapper missing `Unwrap`

**v0.1.0 status:** Disabled after external calibration. An error-valued field
does not prove that the enclosing type intends transparent wrapping; deliberate
opacity is a valid API contract. The retained predicate emits no finding.

**Source:** "Implement `Unwrap() error` so a wrapper joins the chain... a single `fmt.Errorf` may
carry..." / Common Mistakes: "Custom wrapper type missing `Unwrap` | Implement `Unwrap() error`, or
`errors.Is`/`As` stop at this type."

**Why checkable:** A struct type with an `error`-typed field, that has an `Error() string` method
(so it's a self-declared error wrapper) but no `Unwrap() error` method, is exactly the antipattern
— fully resolvable via `go/types` method sets, no judgment call.

**Node type(s):** `*ast.TypeSpec` + `*ast.StructType`, `go/types.MethodSet`.

```go
func hasMethod(t types.Type, name string) bool {
	ms := types.NewMethodSet(types.NewPointer(t))
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Obj().Name() == name {
			return true
		}
	}
	return false
}

func structWrapsErrorWithoutUnwrap(pass *analysis.Pass, ts *ast.TypeSpec, st *ast.StructType) bool {
	hasErrField := false
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue
		}
		if astutil.TypeString(pass, f.Type) == "error" {
			hasErrField = true
		}
	}
	if !hasErrField {
		return false
	}
	obj := pass.TypesInfo.Defs[ts.Name]
	if obj == nil {
		return false
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return false
	}
	return hasMethod(named, "Error") && !hasMethod(named, "Unwrap")
}
```

**Exclusions:** A struct with an `error` field but no `Error()` method isn't a wrapper type at all
(just a struct that happens to hold an error) — excluded via the `hasErrField && hasMethod(...,
"Error")` guard. `Is(target error) bool` and `As(target any) bool` overrides (rule 14's other two
methods) are NOT checked — the source doc says implement them "only when callers extract your type
through a wrapper" (a caller-need judgment call, same escape hatch as `errors-08`); only the
unconditional `Unwrap` requirement is checkable.

**Finding.Message:** `"%s wraps an error field but has no Unwrap() error method at %s — errors.Is/As stop at this type"`

**Finding.Severity:** `SeverityWarning` — breaks chain traversal for callers up the stack but
doesn't crash or lose data by itself; the caller-visible symptom is a failed `errors.Is` check,
which is a debugging cost, not an outage.

---

<a id="errors-15"></a>
### errors-15 — goroutine without recover boundary

**Source:** "Recover panics at goroutine and request boundaries... Wrap every goroutine entry point
in a recover that logs and reports; an unrecovered panic in one goroutine kills the whole process."

**Why checkable:** A `go` statement whose called function is a literal (`*ast.FuncLit`) can be
inspected directly for a `defer func() { ...recover()... }()` as the first-class construct at the
top of its body — this is the idiomatic, universally-recommended shape, and its absence is exact.

**Node type(s):** `*ast.GoStmt` → `.Call.Fun` (`*ast.FuncLit`), `.Body.List[0]` → `*ast.DeferStmt`
→ `*ast.FuncLit`, nested `recover()` call.

```go
func hasDeferredRecover(body *ast.BlockStmt) bool {
	found := false
	for _, stmt := range body.List {
		ds, ok := stmt.(*ast.DeferStmt)
		if !ok {
			continue
		}
		lit, ok := ds.Call.Fun.(*ast.FuncLit)
		if !ok {
			continue
		}
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "recover" {
					found = true
				}
			}
			return true
		})
	}
	return found
}

func goroutineMissingRecover(gs *ast.GoStmt) bool {
	lit, ok := gs.Call.Fun.(*ast.FuncLit)
	if !ok {
		return false // named-function goroutines: recover boundary lives in the callee, out of reach at the call site
	}
	return !hasDeferredRecover(lit.Body)
}
```

**Exclusions:** `go namedFunc(...)` (not a literal) is excluded — the recover boundary, if any,
lives inside `namedFunc`'s own body, which this call-site predicate can't see without a
whole-program call graph; a separate, symmetric rule on `*ast.FuncDecl` bodies used exclusively as
goroutine entry points would need cross-function analysis out of scope for a single-pass check.
`hasDeferredRecover` only scans top-level `DeferStmt`s in `body.List` (not nested inside an inner
`if`/`for`) — matches the idiom of declaring the recover defer as the first statement; a recover
buried conditionally deeper in the body is a documented narrowing, not a silent gap.

**Finding.Message:** `"goroutine spawned at %s has no deferred recover — an unrecovered panic here kills the whole process"`

**Finding.Severity:** `SeverityError` — matches `go.md`'s own goroutine-lifecycle discipline and
`go-security.md`'s explicit recover-boundary requirement on the parsing path; process death from
one unrecovered goroutine panic takes down every concurrent job in that worker.

---

<a id="errors-16"></a>
### errors-16 — `defer Close()` drops the error

**Source:** "Capture deferred close errors with named returns. A bare `defer rc.Close()` drops the
error that, on a writable resource, is the flush failure."

**Why checkable:** `defer <expr>.Close()` with zero arguments, where `<expr>`'s type has a
`Close() error` method (verified via `go/types` method set, not name-only), inside a function
whose result list includes an `error`, is the exact antipattern from the doc's own Trap #6.

**Node type(s):** `*ast.DeferStmt` → `.Call.Fun` (`*ast.SelectorExpr`, `.Sel.Name == "Close"`),
enclosing `*ast.FuncDecl` via `enclosingFuncDecl`.

```go
func closeReturnsError(pass *analysis.Pass, recv ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(recv)
	if t == nil {
		return false
	}
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		m := ms.At(i).Obj()
		if m.Name() != "Close" {
			continue
		}
		sig, ok := m.Type().(*types.Signature)
		return ok && sig.Params().Len() == 0 && sig.Results().Len() == 1 && sig.Results().At(0).Type().String() == "error"
	}
	return false
}

func funcDeclHasErrorReturn(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Type.Results == nil {
		return false
	}
	for _, f := range fn.Type.Results.List {
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == "error" {
			return true
		}
	}
	return false
}

func isBareDeferClose(pass *analysis.Pass, ds *ast.DeferStmt, fn *ast.FuncDecl) bool {
	if !funcDeclHasErrorReturn(fn) {
		return false
	}
	sel, ok := ds.Call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" || len(ds.Call.Args) != 0 {
		return false
	}
	return closeReturnsError(pass, sel.X)
}
```

**Exclusions:** If the enclosing function has no `error` result at all, dropping the close error is
unavoidable (there's nowhere to return it) — excluded via `funcDeclHasErrorReturn`. `Close()`
methods returning zero results (e.g. some `io.Closer`-like types that never error) don't match
`closeReturnsError`'s signature check.

**Finding.Message:** `"defer %s.Close() at %s drops the close error — use a named return and assign it in the deferred func when err == nil"`

**Finding.Severity:** `SeverityError` — on a writable resource this is a silent data-loss path (the
flush failure is exactly what's dropped); matches `go-security.md`'s zero-tolerance stance on
silently ignored errors.

---

<a id="errors-17"></a>
### errors-17 — retry classification by string match

**v0.1.0 status:** Disabled after external calibration. Tests and compatibility
protocols repeatedly had no typed or sentinel error contract available. The
retained predicate emits no finding.

**Source:** "Classify retryable versus terminal errors with data, not string matching... A
`RetryableError` type or a `Temporary() bool` method lets retry loops branch on type;
`strings.Contains(err.Error(), \"timeout\")` breaks the moment wording changes and misses wrapped
errors."

**Why checkable:** Identical AST shape to `errors-12` (`strings.Contains(<err>.Error(), <lit>)`),
disambiguated from it by the enclosing function's single-`bool`-result shape via
`classifyStringMatchRule` (defined under `errors-12` above — full predicate shown there, not
repeated per the anti-duplication note in the shared-predicate callout).

**Node type(s):** Same as `errors-12`: `*ast.CallExpr` (`strings.Contains` + nested `.Error()`),
plus enclosing `*ast.FuncDecl` for classification.

**Predicate:** `isErrorStringMatchCall(pass, call) && classifyStringMatchRule(enclosingFuncDecl(stack)) == "errors-17"` — see `errors-12`'s code block for both functions in full.

**Exclusions:** Same as `errors-12` (Contains-only, not HasPrefix/==). Additionally: the
bool-result heuristic misclassifies a genuinely boundary-mapping function that happens to return
`bool` (e.g. `func isNotFound(err error) bool`) as `errors-17` — documented narrowing; both rule
IDs describe the same underlying antipattern (string-matching `.Error()` output) so a
misclassification between the two still produces a correct, actionable finding, just under the
"wrong" of two closely-related IDs.

**Finding.Message:** `"retry decision at %s classifies by strings.Contains(err.Error(), ...) — use errors.Is(err, ...) or a typed RetryableError/Temporary() method"`

**Finding.Severity:** `SeverityWarning` — same real-world fragility as `errors-12` (breaks on
wording change, misses wrapped errors), same severity tier.

---

<a id="errors-18"></a>
### errors-18 — EXCLUDED: raw error string as metric label

**Source:** "Label error metrics by a stable code bucket, not the raw error string.
`errors_total{code=\"not_found\"}` is bounded; `errors_total{msg=\"...\"}` is an unbounded series
that grows with every wrapped context and starves the metrics backend."

**Why excluded:** Detecting this requires recognizing a specific, undeclared metrics library's
call shape (Prometheus client, OpenTelemetry metrics API, or an internal wrapper — this project's
stack lock names Grafana as the dashboard but not a specific Go metrics client library) and then
proving that a *specific argument position* in that call is (a) a label value and (b) sourced from
`err.Error()` or a `%v`/`%w`-formatted error string, as opposed to any other legitimate string
label (a `code` field, a `status` string, a `route` name). A library-agnostic proxy — "flag any
call passing `err.Error()` as a string argument to anything" — would fire on the overwhelming
majority of legitimate logging calls (`log.Error("msg", "err", err.Error())`) that are not metrics
at all, producing unacceptable false-positive volume with no reliable way to distinguish "this
argument became a Prometheus label" from "this argument is a structured log field" using syntax
alone. Excluded rather than approximated with a library-specific or fragile heuristic; a future
domain file (Observability, `phase-4a-observability.md`) could re-scope this narrowly if it pins a
specific metrics client import path, but that's out of this file's self-contained scope.

---

<a id="errors-19"></a>
### errors-19 — `os.Exit`/`log.Fatal*` outside `main`

**Source:** project Go convention: "`os.Exit` and `log.Fatal` only in
`main()`. All other functions return errors." This rule is not one of `error-handling.md`'s 18
source rules; it is appended
to this domain because it's an error-handling call-site rule like every other rule here.

**Why checkable:** `astutil.IsPkgFunc` gives an exact, non-judgmental package-qualified match for
`os.Exit` and each of `log.Fatal`/`Fatalf`/`Fatalln` — no semantic guessing about whether a call
"looks like" a fatal exit. The enclosing-function and enclosing-package checks are exact resolved
facts (`enclosingFuncDecl`, `pass.Pkg.Name()`), the same technique `errors-10`'s `isStartupContext`
already uses for its own main/init carve-out.

**Node type(s):** `*ast.CallExpr` (`os.Exit`, or `log.Fatal`/`Fatalf`/`Fatalln`, resolved via
`astutil.IsPkgFunc`), enclosing `*ast.FuncDecl` via `enclosingFuncDecl`.

```go
func isExitOrFatalCall(pass *analysis.Pass, call *ast.CallExpr) (name string, ok bool) {
	if astutil.IsPkgFunc(pass, call, "os", "Exit") {
		return "os.Exit", true
	}
	for _, fn := range [...]string{"Fatal", "Fatalf", "Fatalln"} {
		if astutil.IsPkgFunc(pass, call, "log", fn) {
			return "log." + fn, true
		}
	}
	return "", false
}

func isAllowedExitCallSite(pass *analysis.Pass, pos token.Pos, fn *ast.FuncDecl) bool {
	if pass.Pkg.Name() == "main" || pass.Pkg.Name() == "cmd" ||
		strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go") {
		return true
	}
	if fn == nil {
		return false
	}
	if strings.HasPrefix(fn.Name.Name, "Fatal") {
		return true
	}
	if fn.Name.Name != "TestMain" || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	return len(param.Names) == 1 && astutil.TypeString(pass, param.Type) == "*testing.M"
}
```

**Exclusions:** all calls in package `main`, package `cmd`, and `_test.go` are excluded: calibration
found terminal command dispatch and test scaffolding rather than reusable library behavior. A valid
`TestMain(m *testing.M)` is also accepted. Functions whose names begin with `Fatal` are excluded
because they intentionally implement a fatal-logger contract. The retained findings are calls from
ordinary reusable package functions where callers cannot recover or run deferred cleanup.

**Finding.Message:** `"%s called at %s outside func main in package main (or TestMain) — os.Exit/log.Fatal* skip deferred cleanup up the call stack and make the call untestable by any caller who wanted to handle the failure gracefully"`

**Finding.Severity:** `SeverityError` — an `os.Exit`/`log.Fatal*` call deep in a library function
skips all deferred cleanup up the call stack (deferred closes, unlocks, flushes) and makes the
function's behavior untestable by any caller who wanted to handle the failure gracefully; in this
project's own audited input-processing code (though this tool audits arbitrary target repos) this
is exactly the kind of call that turns a recoverable parse error into a full
process crash for every concurrent request being served, not just the one that failed.

---

## 3. Fixture file spec

The fixture material below is retained for future redesign; it is not the v0.1.0 active inventory.
Directory: `internal/tools/testdata/fixtures/audit-errors/`. One Go package per documented rule
(`rule01/` .. `rule07/`, `rule09/` .. `rule17/`, `rule19/` — 17 directories; no `rule08`/`rule18`
since those rules are excluded and never registered, so no fixture exists for an unregistered
rule ID), per
`contracts.md`'s Testdata fixture layout. Each rule package contains exactly one `violation.go`
(the true-positive case) and, where a false-positive risk exists, one `compliant.go` (the
near-miss) — never a single file mixing both, so a stray finding can never be misattributed to a
sibling rule. `// VIOLATION: errors-NN` / `// COMPLIANT: errors-NN` markers sit directly above the
relevant line. Where `violation.go` and `compliant.go` share a same-package symbol (e.g. rule03's
`lookup`), it's declared once, in `violation.go`; `compliant.go` uses it directly via same-package
visibility — no import needed, and no third support file invented. No credential-shaped numeric
literals anywhere; any secret-like example uses placeholder tokens (`"tok_xxx"`, `"item-123"`),
though none of the 17 rules below happen to need one.

**`rule01/violation.go`** (errors-01)
```go
package rule01

// VIOLATION: errors-01
func FetchBad() (error, string) {
	return nil, "ok"
}
```

**`rule01/compliant.go`**
```go
package rule01

// COMPLIANT: errors-01
func FetchGood() (string, error) {
	return "ok", nil
}
```

**`rule02/violation.go`** (errors-02)
```go
package rule02

import "fmt"

type ParseError struct{ Field, Reason string }

func (e *ParseError) Error() string { return fmt.Sprintf("field %s: %s", e.Field, e.Reason) }

// VIOLATION: errors-02
func ParseBad(data []byte) (string, *ParseError) {
	return "", nil
}
```

**`rule02/compliant.go`**
```go
package rule02

// COMPLIANT: errors-02
func ParseGood(data []byte) (string, error) {
	return "", nil
}
```

**`rule03/violation.go`** (errors-03)
```go
package rule03

import "fmt"

func lookup(id string) (string, error) { return "", nil }

func LoadBad(id string) (string, error) {
	v, err := lookup(id)
	// VIOLATION: errors-03
	if err != nil {
		return "", fmt.Errorf("looking up %s: %w", id, err)
	} else {
		return v, nil
	}
}
```

**`rule03/compliant.go`**
```go
package rule03

import "fmt"

func LoadGood(id string) (string, error) {
	v, err := lookup(id)
	// COMPLIANT: errors-03
	if err != nil {
		return "", fmt.Errorf("looking up %s: %w", id, err)
	}
	return v, nil
}
```

**`rule04/violation.go`** (errors-04)
```go
package rule04

type logger struct{}

func (logger) Error(msg string, args ...any) {}

var log logger

func getUser(id string) (string, error) { return "", nil }

func ProcessBad(ctx context.Context, id string) error {
	user, err := getUser(id)
	if err != nil {
		// VIOLATION: errors-04
		log.Error("getting user", "err", err)
		return err
	}
	_ = user
	return nil
}
```

**`rule04/compliant.go`**
```go
package rule04

import (
	"context"
	"fmt"
)

func ProcessGood(ctx context.Context, id string) error {
	user, err := getUser(id)
	if err != nil {
		// COMPLIANT: errors-04
		return fmt.Errorf("getting user %s: %w", id, err)
	}
	_ = user
	return nil
}
```

`rule04/violation.go` needs `"context"` too (for `ProcessBad`'s parameter) — both files import it;
only `compliant.go` additionally needs `"fmt"`.

**`rule05/violation.go`** (errors-05)
```go
package rule05

func persist(id string) error { return nil }

func SaveBad(id string) error {
	err := persist(id)
	if err != nil {
		// VIOLATION: errors-05
		return err
	}
	return nil
}
```

**`rule05/compliant.go`**
```go
package rule05

import "fmt"

func SaveGood(id string) error {
	err := persist(id)
	if err != nil {
		// COMPLIANT: errors-05
		return fmt.Errorf("persisting %s: %w", id, err)
	}
	return nil
}
```

Note: `rule04/violation.go`'s log-then-return block also contains a bare `return err`. Per
`errors-05`'s documented exclusion (skip if the block also matches `errors-04`'s predicate), that
line is NOT double-counted as an `errors-05` finding when `errors.Analyzer` runs over `rule04/` —
verified explicitly in `TestAuditErrors_Rule05` below, which runs `errors.Analyzer` over `rule05/`
and asserts the single `errors-05` finding is located in `rule05/violation.go`.

**`rule06/violation.go`** (errors-06)
```go
package rule06

import "fmt"

func lookupUser(id string) (string, error) { return "", nil }

func FetchUserBad(id string) (string, error) {
	_, err := lookupUser(id)
	if err != nil {
		// VIOLATION: errors-06
		return "", fmt.Errorf("failed to fetch user: %w", err)
	}
	return "", nil
}
```

**`rule06/compliant.go`**
```go
package rule06

import "fmt"

func FetchUserGood(id string) (string, error) {
	_, err := lookupUser(id)
	if err != nil {
		// COMPLIANT: errors-06
		return "", fmt.Errorf("fetching user %s: %w", id, err)
	}
	return "", nil
}
```

**`rule07/violation.go`** (errors-07)
```go
package rule07

import "errors"

// VIOLATION: errors-07
var ErrBadStyle = errors.New("Invalid message ID.")
```

**`rule07/compliant.go`**
```go
package rule07

import "errors"

// COMPLIANT: errors-07
var ErrGoodStyle = errors.New("invalid message id")
```

**`rule09/violation.go`** (errors-09)
```go
package rule09

import "io"

func CopyBad(dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	// VIOLATION: errors-09
	_ = err
}
```

**`rule09/compliant.go`**
```go
package rule09

import "io"

func CopyGood(dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	// COMPLIANT: errors-09 — best-effort diagnostics copy, failure isn't actionable here
	_ = err
}
```

**`rule10/violation.go`** (errors-10)
```go
package rule10

func ValidateBad(n int) {
	if n < 0 {
		// VIOLATION: errors-10
		panic("negative count")
	}
}
```

**`rule10/compliant.go`**
```go
package rule10
func ValidateGood(n int) {
	// COMPLIANT: errors-10 — panic shadowed by a local closure, not the
	// builtin; exercises isPanicCall's *types.Builtin guard directly.
	panic := func(v any) {}
	if n < 0 {
		panic("negative count")
	}
}
```

Note: this rule's `main`/`init`/`package main` exclusion cannot be exercised inside `rule10/`
(never `package main`). That branch is validated by code review of `isStartupContext` against
`phase-4a-concurrency.md`-style predicate review, not a fixture case here — documented gap, not a silent
one (see `errors-10`'s Exclusions above).

**`rule11/violation.go`** (errors-11)
```go
package rule11

type Config struct{ Addr string }

func MustLoadConfig() *Config { return &Config{Addr: "localhost"} }

var cfg = MustLoadConfig() // package-level init: allowed, no enclosing FuncDecl

func HandleBad(req string) *Config {
	// VIOLATION: errors-11
	return MustLoadConfig()
}
```

**`rule11/compliant.go`**
```go
package rule11

func main() {
	// COMPLIANT: errors-11 — Must* called from a function literally named main
	_ = MustLoadConfig()
}
```

**`rule12/violation.go`** (errors-12)
```go
package rule12

import (
	"errors"
	"strings"
)

var ErrNotFound = errors.New("not found")

func StatusCodeBad(err error) int {
	// VIOLATION: errors-12
	if strings.Contains(err.Error(), "not found") {
		return 404
	}
	return 500
}
```

**`rule12/compliant.go`**
```go
package rule12

import "errors"

func StatusCodeGood(err error) int {
	// COMPLIANT: errors-12
	if errors.Is(err, ErrNotFound) {
		return 404
	}
	return 500
}
```

**`rule13/violation.go`** (errors-13)
```go
package rule13

import "fmt"

func ConnectBad(host string, err1, err2 error) error {
	// VIOLATION: errors-13
	return fmt.Errorf("connecting to %s: %w and %w", host, err1, err2)
}
```

**`rule13/compliant.go`**
```go
package rule13

import "fmt"

func ConnectGood(host string, err error) error {
	// COMPLIANT: errors-13
	return fmt.Errorf("connecting to %s: %w", host, err)
}
```

**`rule14/violation.go`** (errors-14)
```go
package rule14

import "fmt"

// VIOLATION: errors-14
type BadWrapper struct {
	Op  string
	Err error
}

func (e *BadWrapper) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
```

**`rule14/compliant.go`**
```go
package rule14

import "fmt"

// COMPLIANT: errors-14
type GoodWrapper struct {
	Op  string
	Err error
}

func (e *GoodWrapper) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *GoodWrapper) Unwrap() error { return e.Err }
```

**`rule15/violation.go`** (errors-15)
```go
package rule15

import "context"

func process(ctx context.Context) {}

func SpawnBad(ctx context.Context) {
	// VIOLATION: errors-15
	go func() {
		process(ctx)
	}()
}
```

**`rule15/compliant.go`**
```go
package rule15

import "context"

func SpawnGood(ctx context.Context) {
	// COMPLIANT: errors-15
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = r
			}
		}()
		process(ctx)
	}()
}
```

**`rule16/violation.go`** (errors-16)
```go
package rule16

import (
	"fmt"
	"os"
)

func WriteAllBad(path, p string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	// VIOLATION: errors-16
	defer f.Close()
	if _, err := f.WriteString(p); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}
```

**`rule16/compliant.go`**
```go
package rule16

import (
	"fmt"
	"os"
)

func WriteAllGood(path, p string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	// COMPLIANT: errors-16
	defer func() {
		if c := f.Close(); c != nil && err == nil {
			err = fmt.Errorf("closing file: %w", c)
		}
	}()
	if _, err = f.WriteString(p); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}
```

**`rule17/violation.go`** (errors-17)
```go
package rule17

import "strings"

func ShouldRetryBad(err error) bool {
	// VIOLATION: errors-17
	return strings.Contains(err.Error(), "timeout")
}
```

**`rule17/compliant.go`**
```go
package rule17

import (
	"context"
	"errors"
)

func ShouldRetryGood(err error) bool {
	// COMPLIANT: errors-17
	return errors.Is(err, context.DeadlineExceeded)
}
```

**`rule19/violation.go`** (errors-19)
```go
package rule19

import "os"

func ShutdownBad(code int) {
	// VIOLATION: errors-19
	os.Exit(code)
}
```

**`rule19/compliant.go`**
```go
package rule19

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// COMPLIANT: errors-19 — TestMain is the test binary's own entrypoint;
	// os.Exit(m.Run()) here is the documented idiomatic pattern, independent
	// of package name.
	os.Exit(m.Run())
}
```

Note: this rule's other exclusion — `func main()` inside `package main` — cannot be exercised
inside `rule19/` (never `package main`), the identical structural gap `errors-10`'s fixture note
already documents for its own main/init carve-out. That branch is validated by code review of
`isMainEntrypoint` against `errors-10`'s `isStartupContext`, not a fixture case here — a
documented gap, not a silent one.

---

## 4. Tool file spec

### Analysis subpackage — `internal/analysis/errors/errors.go`

Pasted verbatim from `contracts.md`'s conformance block, substituting `errors` for `<domain>`.
The 17 registrations comprise three active `RegisterRule` calls and fourteen calibrated-off
`RegisterDisabledRule` calls. The full dispatch across all 17 predicates from section 2 is the per-rule elaboration of the single
representative `astutil.Report` call shown below — this is the shape, not a re-enumeration of
section 2.

```go
package errors // never package analysis — see Naming and file layout in contracts.md

import (
	"go/ast"
	"go/token" // errCheckIdent's token.NEQ

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
	astutil.RegisterRule("errors-04", "log_and_return", finding.SeverityError)
	// one RegisterRule call per active rule in this domain (errors-04, -09, -19 — matching
	// section 1's active inventory), plus one RegisterDisabledRule call per
	// calibrated-off rule. Excluded rules are never registered. astutil.Report suppresses disabled
	// rules and panics at analyzer-init/test time on an unregistered
	// rule ID, by design: a typo'd rule ID must fail loud in `go test ./...`, never ship
	// silently with an empty Severity.
}

var Analyzer = &analysis.Analyzer{
	Name:     "errors",
	Doc:      "finds precise, mechanical error-handling problems",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{ /* node types section 2 cares about, per rule */ }, func(n ast.Node) {
		// per-rule detection predicate from section 2; on match:
		astutil.Report(pass, n.Pos(), "errors-NN", "<message template %s>", arg)
	})
	return nil, nil // findings are collected via pass.Report -> Action.Diagnostics, not the return value
}
```

`Analyzer` is the **only** exported symbol from this subpackage. This domain does not declare
`RunErrors`, `ErrorsAnalyzer`, or any other entry point, driver, or `Run<Domain>`/
`run<Domain>Pass`/`mustRun<Domain>` wrapper — the single `audit.Run` call in the tool handler below
is the only call site, per `contracts.md`.

### Orchestration

`internal/audit.Run(ctx, ws, pattern, analyzers)` is the one project-wide entry point — reproduced,
unchanged, from `contracts.md`'s Orchestration section. The errors domain contributes
`errors.Analyzer` to that call; it implements no `packages.Load`, no `checker.Analyze`, and no
dedup logic of its own.

### Tool file — `internal/tools/go_audit_errors.go`

`CacheTTL: 0` (same as `go_audit_concurrency`/`go_audit_performance` — analysis output goes stale
the instant source changes).

```go
package tools

import (
	"context"
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditErrorsInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default is all severities"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditErrorsOutput struct {
	Result finding.AuditResult `json:"result"`
}

func AuditErrorsHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditErrorsInput) (*mcp.CallToolResult, AuditErrorsOutput, error) {
	if err := normalizeAuditErrorsInput(&in); err != nil {
		return nil, AuditErrorsOutput{}, fmt.Errorf("validating input: %w", err)
	}

	ws, err := resolveInWorkspace(in.Package)
	if err != nil {
		return nil, AuditErrorsOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{errors.Analyzer})
	if err != nil {
		return nil, AuditErrorsOutput{}, fmt.Errorf("running errors audit: %w", err)
	}
	result = finding.Filter(result, in.MinSeverity, in.MaxFindings)

	return nil, AuditErrorsOutput{Result: result}, nil
}

func RegisterAuditErrors(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_audit_errors",
		Description: "Runs the active errors-04, -09, and -19 go/analysis passes over a package and returns findings.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, AuditErrorsHandler)
}
func normalizeAuditErrorsInput(in *AuditErrorsInput) error {
	if in.Package == "" {
		return fmt.Errorf("package is required")
	}
	if in.MaxFindings < 0 {
		return fmt.Errorf("max_findings must not be negative")
	}
	if in.MaxFindings == 0 {
		in.MaxFindings = 200
	}
	if in.MaxFindings > 1000 {
		in.MaxFindings = 1000
	}
	if in.MinSeverity == "" {
		in.MinSeverity = finding.SeverityInfo
	}
	return finding.ValidateSeverity(in.MinSeverity)
}
```

`resolveInWorkspace` is the single project-wide input-containment helper (`contracts.md`'s
Input containment and resource limits) — not redeclared here. Every `Finding.Rule` value this tool
can emit is one of the 3 active IDs in section 1. Calibrated-off IDs
`errors-01`, `errors-02`, `errors-03`, `errors-05`, `errors-06`, `errors-07`, `errors-10`,
`errors-11`, `errors-12`, `errors-13`, `errors-14`, `errors-15`, `errors-16`, and `errors-17`
are registered as disabled metadata and suppressed by
`astutil.Report`. Excluded IDs `errors-08` and `errors-18` are never registered or computed.

---

## 5. Verification

The v0.1.0 suite runs positive and near-miss fixtures for the three active
rules, asserts the fourteen calibrated-off IDs separately, and keeps a standing
exclusion guard in `internal/analysis/errors/audit_test.go`. Isolated packages
live under `internal/analysis/errors/testdata/audit/rule<NN>/` and are loaded
through `audit.Run`. Dedicated `audit-main` and `audit-cmd` modules plus
`_test.go` and fatal-logger fixtures lock in the calibrated `errors-04` and
`errors-19` exclusions. The older code sketches below illustrate the intended
assertion shape; the paths and helpers in the executable test are canonical.

Future-redesign example (`errors-01`, disabled in v0.1.0):

```go
func TestAuditErrors_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, errors.Analyzer, "audit-errors/rule01")

	require.Len(t, findings, 1, "expected exactly one errors-01 finding")
	f := findings[0]
	assert.Equal(t, "errors-01", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-errors/rule01/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line) // `func FetchBad() (error, string) {`
}

func TestAuditErrors_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, errors.Analyzer, "audit-errors/rule01")
	for _, f := range findings {
		assert.NotEqual(t, "errors-01", f.Rule, "compliant.go must not trigger its own rule")
	}
}
```

The active rules `errors-04`, `errors-09`, and `errors-19` apply the positive/near-miss expectations: each asserts a `Finding` with that
rule ID, located in
`fixtures/audit-errors/rule<NN>/violation.go` at the real violating line, and verifies that
`compliant.go`'s near-miss never contributes a finding for that rule.

If the calibrated-off rules below are redesigned and re-enabled, they also require these
cross-package assertions:

- **`errors-05`**: `rule04/violation.go`'s log-then-return block also contains a bare `return err`,
  which must NOT additionally surface as an `errors-05` finding — this is the exclusion documented
  under `errors-05` (skip if the block also matches `errors-04`). `TestAuditErrors_Rule05` asserts
  the single `errors-05` finding across `audit-errors/rule05` is located in
  `fixtures/audit-errors/rule05/violation.go`, never `rule04/violation.go`.
- **`errors-12`/`errors-17`**: must additionally assert cross-classification doesn't leak —
  running `errors.Analyzer` over `audit-errors/rule12` must NOT also produce an `errors-17`
  finding, and running it over `audit-errors/rule17` must NOT also produce an `errors-12` finding
  (proves `classifyStringMatchRule`'s bool-result disambiguation actually partitions the two rule
  IDs rather than double-emitting).

Domain-wide rule count:

```go
func TestAuditErrors_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("errors"), 3) // catches an active rule silently dropped or added
}
```

Standing regression guard — `errors-08`/`errors-18` are excluded and never registered, so running
the analyzer over the whole fixture tree must never surface either ID, from any rule's fixtures:

```go
func TestAuditErrors_ExcludedRulesNotEmitted(t *testing.T) {
	findings := astutil.RunFixture(t, errors.Analyzer, "audit-errors")
	assert.Empty(t, astutil.FindingsForRule(findings, "errors-08"), "errors-08 (%%w vs %%v) is excluded — no predicate should ever emit it")
	assert.Empty(t, astutil.FindingsForRule(findings, "errors-18"), "errors-18 (metric label raw error string) is excluded — no predicate should ever emit it")
}
```
