# Phase 4 — Security Static Analysis (`internal/analysis/security/`)

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

The original security/observability corpus is reproduced in this specification;
no external reference file is required.
Scope: SECURITY half only (data leakage, crypto misuse, injection, secrets, unsafe input).
Observability-only rules (slog config, span creation, metric cardinality, liveness/readiness)
are excluded here — covered by the observability pass. Ambiguous rules noted with a
one-line cross-reference instead of duplicated detail.

Recipe followed per `contracts.md` §"Rule → AST-pattern conversion recipe". Extends
`Finding`/`AuditResult`/`Severity`/`Location` and the pass skeleton/`astutil` surface from
`contracts.md`'s Conformance block. Does not redefine them.

## Shared helpers

Domain-specific declarations live in `internal/analysis/security/security.go`, `package
security` — never `package analysis` (self-collides with the `golang.org/x/tools/go/analysis`
import; see `contracts.md`'s Naming and file layout). This domain no longer declares
`report`, `calleeName`, or `unquote`: detection now goes through `astutil.Report`,
`astutil.IsPkgFunc`, `astutil.IsMethodOn`, and `astutil.StringLit`. The helpers below are
genuinely security-domain logic with no `astutil` equivalent (no other domain needs a
secret-name regex, an AEAD/SQL/DynamoDB call-shape matcher, or a `Stringer`/`LogValuer`
method-set check).

```go
package security

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
	astutil.RegisterRule("security-01", "security.unmasked_secret_struct", finding.SeverityError)
	astutil.RegisterRule("security-02", "security.math_rand_secret_token", finding.SeverityError)
	astutil.RegisterRule("security-03", "security.weak_password_hash", finding.SeverityError)
	astutil.RegisterRule("security-04", "security.static_aead_nonce", finding.SeverityError)
	astutil.RegisterRule("security-05", "security.timing_unsafe_secret_compare", finding.SeverityWarning)
	astutil.RegisterRule("security-06", "security.hardcoded_credential", finding.SeverityError)
	astutil.RegisterRule("security-07", "security.secret_in_log_or_error", finding.SeverityError)
	astutil.RegisterRule("security-08", "security.whole_struct_logged", finding.SeverityWarning)
	astutil.RegisterRule("security-09", "security.sql_string_concat", finding.SeverityError)
	astutil.RegisterRule("security-10", "security.dynamodb_expression_concat", finding.SeverityError)
	astutil.RegisterRule("security-11", "security.regexp_dynamic_pattern", finding.SeverityInfo)
	astutil.RegisterRule("security-12", "security.pii_in_span_attribute", finding.SeverityWarning)
	astutil.RegisterRule("security-13", "security.integer_narrowing_conversion", finding.SeverityWarning)
	astutil.RegisterRule("security-14", "security.unclosed_http_response_body", finding.SeverityError)
}

var Analyzer = &analysis.Analyzer{
	Name:     "security",
	Doc:      "checks Go source for secret leakage, weak crypto, injection, and PII exposure",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}
```

`run(pass *analysis.Pass) (interface{}, error)` dispatches to each rule's own `insp.Preorder`
block shown in its own §2 entry below, in rule-ID order, ending `return nil, nil` per the
canonical pass skeleton — not reproduced as one mega-function here; see §4's "Analyzer identity
and rule→predicate wiring" table for the node-type-to-checker dispatch map.

```go
// secretField matches identifier names that plausibly hold secret/credential material.
// Shared by security-01, 02, 05 (via secretName), 06 (via credentialName's stricter variant),
// and 07 — one regex, not a near-duplicate per rule.
var secretField = regexp.MustCompile(`(?i)(secret|password|token|apikey|api_key|credential|privatekey)`)

// baseIdent returns the base *ast.Ident of e if e is an identifier or a selector expression
// rooted at one (e.g. `x` in both `x` and `x.Field`), or nil otherwise. Shared by security-02,
// 03, 07 to name-gate a call argument/assignment target without full dataflow.
func baseIdent(e ast.Expr) *ast.Ident {
	switch v := e.(type) {
	case *ast.Ident:
		return v
	case *ast.SelectorExpr:
		return baseIdent(v.X)
	default:
		return nil
	}
}

// implementsStringerOrLogValuer reports whether t's method set includes both String() string
// and LogValue() slog.Value — the masking contract security-01, 07, and 08 all check for.
// Resolved via go/types against the real declared method set, not a hardcoded name list.
func implementsStringerOrLogValuer(t types.Type) bool {
	ms := types.NewMethodSet(types.NewPointer(t))
	return ms.Lookup(nil, "String") != nil && ms.Lookup(nil, "LogValue") != nil
}

// loggingFuncs is the slog.*/log.*/fmt.Print* family checked by security-07 and security-08.
// security-07 additionally matches fmt.Errorf, checked separately since it's an error
// constructor, not a log call.
var loggingFuncs = []struct{ pkg, name string }{
	{"log/slog", "Info"}, {"log/slog", "InfoContext"},
	{"log/slog", "Warn"}, {"log/slog", "WarnContext"},
	{"log/slog", "Error"}, {"log/slog", "ErrorContext"},
	{"log/slog", "Debug"}, {"log/slog", "DebugContext"},
	{"log", "Print"}, {"log", "Printf"}, {"log", "Println"},
	{"fmt", "Print"}, {"fmt", "Printf"}, {"fmt", "Println"},
}

// matchedLoggingFunc reports the "pkg.Func" display name of the loggingFuncs entry call
// matches, or ok=false if none does.
func matchedLoggingFunc(pass *analysis.Pass, call *ast.CallExpr) (name string, ok bool) {
	for _, f := range loggingFuncs {
		if astutil.IsPkgFunc(pass, call, f.pkg, f.name) {
			short := f.pkg[strings.LastIndex(f.pkg, "/")+1:]
			return short + "." + f.name, true
		}
	}
	return "", false
}
```

Rule-specific helpers too narrow for reuse (e.g. security-06's `credentialName`/
`isNonEmptyStringLit`, security-10's `dynamoExprFields`/`isDynamoDBInputStruct`/
`unwrapAWSPointerHelper`) are shown inline in that rule's predicate section below.

---

## 1. Rules table

| Rule ID (`Finding.Rule`) | `RuleName` (dotted slug, `astutil.RegisterRule`'s 2nd arg) | One-line description |
|---|---|---|
| `security-01` | `security.unmasked_secret_struct` | Struct with a secret-shaped field lacks `String()`/`LogValue()` masking |
| `security-02` | `security.math_rand_secret_token` | `math/rand` output flows into a secret/token/nonce/session-named identifier |
| `security-03` | `security.weak_password_hash` | MD5/SHA family used to hash a password-shaped value |
| `security-04` | `security.static_aead_nonce` | AEAD `Seal`/`Open` called with a literal (non-generated) nonce |
| `security-05` | `security.timing_unsafe_secret_compare` | `==`/`!=` used to compare secret-shaped values instead of `subtle.ConstantTimeCompare` |
| `security-06` | `security.hardcoded_credential` | String literal assigned directly to a credential-shaped identifier |
| `security-07` | `security.secret_in_log_or_error` | Secret-shaped identifier passed as an argument to a log or `fmt.Errorf` call |
| `security-08` | `security.whole_struct_logged` | Struct-typed value (without `Stringer`/`LogValuer`) passed whole to a log call |
| `security-09` | `security.sql_string_concat` | SQL query built via string concatenation or `fmt.Sprintf` instead of parameters |
| `security-10` | `security.dynamodb_expression_concat` | DynamoDB expression field built via string concatenation instead of expression builders |
| `security-11` | `security.regexp_dynamic_pattern` | `regexp.Compile`/`MustCompile` called with a non-literal (potentially untrusted) pattern |
| `security-12` | `security.pii_in_span_attribute` | Secret/PII-shaped value attached as an OpenTelemetry span attribute |
| `security-13` | `security.integer_narrowing_conversion` | Wider runtime-computed integer narrowed via explicit conversion, no visible bounds check (gosec G115-style) |
| `security-14` | `security.unclosed_http_response_body` | `*http.Response.Body` read but never closed in the same function, leaking the pooled connection |

12 rules extracted from the source document. `security-13` and `security-14` (below) are
analyzer-specific extensions outside that document's scope; see their own §2 entries for
provenance instead of a "Source sentence" quote. 8 source items
excluded with rationale (§2, end). 7 items cross-referenced to a different domain, not
duplicated (§2, end).

## 2. Per-rule detail

### security-01 — `security.unmasked_secret_struct`

**Source sentence:** "Sensitive structs must implement `fmt.Stringer` to mask values. Log
aggregators are rarely compliance-scoped; a single `%v` leaks the whole struct." (rule 1,
reinforced by Critical Trap #1: "Implement BOTH `String()` (for `fmt` verbs) and `LogValue()`
(for slog). A `String()` alone does not protect slog's `%v` path.")

**Checkable:** yes — syntactic (field-name pattern) + semantic (method-set lookup on the
declared type). No dataflow needed.

**Predicate:** `*ast.TypeSpec` wrapping a `*ast.StructType`. Any field name matches
`secretField`, AND the type's method set (via the shared `implementsStringerOrLogValuer`
helper, not a duplicate ad hoc lookup) is missing either `String() string` or
`LogValue() slog.Value`.

```go
insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
	ts := n.(*ast.TypeSpec)
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return
	}
	obj := pass.Pkg.Scope().Lookup(ts.Name.Name)
	if obj == nil {
		return
	}
	for _, f := range st.Fields.List {
		for _, name := range f.Names {
			if !secretField.MatchString(name.Name) {
				continue
			}
			if !implementsStringerOrLogValuer(obj.Type()) {
				astutil.Report(pass, ts.Pos(), "security-01",
					"type %s has secret-shaped field %s but no String()/LogValue() mask",
					ts.Name.Name, name.Name)
			}
		}
	}
})
```

**Exclusions:** skip embedded/anonymous fields (promoted, not a direct leak source at this
type); skip fields already tagged `json:"-"` (not serialized, lower risk — still flag, since
`%v`/slog bypass json tags, but drop to Info if this is the only export path — out of scope
for MVP, keep at declared severity).

**Finding.Message:** `"type %s has secret-shaped field %s but no String()/LogValue() mask"`

**Finding.Severity:** `SeverityError` — production note in source: an unmasked secret field
sat in a shared log aggregator for 12 months before a compliance audit caught it; this is the
exact failure mode the rule exists to prevent.

---

### security-02 — `security.math_rand_secret_token`

**Source sentence:** "Use `crypto/rand`, never `math/rand`, for tokens, nonces, session IDs,
and any secret material. `math/rand` is predictable and exploitable." (rule 2, Critical Trap
#2.)

**Checkable:** yes, with a name-based gate to bound false positives — `math/rand` has
legitimate non-secret uses (jitter, load-balancing, test fixtures), so a blanket flag on every
`math/rand` call is unacceptably noisy. Gate: only flag when the call's result assigns into
an identifier whose name is secret-shaped.

**Predicate:** `*ast.CallExpr` whose callee resolves (via `pass.TypesInfo.Uses`) to a function
in import path `math/rand` OR `math/rand/v2` (`Read`, `Int63`, `Int63n`, `Intn`, `Float64`, …
for `math/rand`; `Int64`, `IntN`, `Uint64`, `Float64`, … for the Go 1.22+ `math/rand/v2`
package — a distinct import path with the same non-cryptographic-RNG problem, not a version of
the same package, so both must be checked explicitly), where the enclosing
`*ast.AssignStmt`/`*ast.ValueSpec` LHS identifier matches `secretField`. `typeutil.StaticCallee`
+ a package-path check is used directly here rather than `astutil.IsPkgFunc` — the predicate
needs "any function in `math/rand` or `math/rand/v2`," not one named function, which is outside
`IsPkgFunc`'s single-name-match design.

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil {
		return
	}
	pkgPath := fn.Pkg().Path()
	if pkgPath != "math/rand" && pkgPath != "math/rand/v2" {
		return
	}
	if lhsName := enclosingAssignTarget(n); lhsName != "" && secretField.MatchString(lhsName) {
		astutil.Report(pass, call.Pos(), "security-02",
			"%s derived from math/rand, which is predictable; use crypto/rand", lhsName)
	}
})
```

`enclosingAssignTarget` walks up via the inspector's parent stack to the nearest
`*ast.AssignStmt`/`*ast.ValueSpec` and returns the LHS identifier name, or `""` if the call
result isn't directly assigned (e.g., passed inline — out of scope, would need dataflow).

**Exclusions:** direct inline use without assignment (e.g., `fmt.Sprintf("%x", rand.Int63())`
per the trap's own WRONG example) is NOT caught by this predicate as written — note as a known
gap rather than widen the heuristic and risk false positives on legitimate jitter code. Widen
only if the fixture regression shows this gap matters in practice: also match when the
`CallExpr` is a direct argument to a call whose callee is `hex.EncodeToString`,
`base64.*.EncodeToString`, or `fmt.Sprintf` (checked via `astutil.IsPkgFunc`/`astutil.IsMethodOn`
per candidate, not a resurrected `calleeName` regex).

**Finding.Message:** `"%s derived from math/rand, which is predictable; use crypto/rand"`

**Finding.Severity:** `SeverityError` — predictable secret material is directly exploitable
for session hijack / token forgery, per source's own framing ("predictable and exploitable").

---

### security-03 — `security.weak_password_hash`

**Source sentence:** "bcrypt or argon2id for password hashing, never MD5 or SHA of a
password." (rule 4; Common Mistakes table: "MD5 or SHA of a password" → `bcrypt.GenerateFromPassword`
or `argon2id`.)

**Checkable:** yes — semantic on callee identity (a closed 8-entry candidate list, checked via
`astutil.IsPkgFunc` instead of the former `typeutil.StaticCallee` + `FullName()` map lookup),
name-gated on the argument.

```go
var weakHashFuncs = []struct{ pkg, name string }{
	{"crypto/md5", "New"}, {"crypto/md5", "Sum"},
	{"crypto/sha1", "New"}, {"crypto/sha1", "Sum"},
	{"crypto/sha256", "New"}, {"crypto/sha256", "Sum256"},
	{"crypto/sha512", "New"}, {"crypto/sha512", "Sum512"},
}

// weakHashFuncName reports the short function name (e.g. "Sum256") if call matches one of
// weakHashFuncs, or ok=false otherwise.
func weakHashFuncName(pass *analysis.Pass, call *ast.CallExpr) (name string, ok bool) {
	for _, f := range weakHashFuncs {
		if astutil.IsPkgFunc(pass, call, f.pkg, f.name) {
			return f.name, true
		}
	}
	return "", false
}

var passwordName = regexp.MustCompile(`(?i)(password|passwd|pwd)`)
```

**Predicate:**

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	name, ok := weakHashFuncName(pass, call)
	if !ok {
		return
	}
	for _, arg := range call.Args {
		if id := baseIdent(arg); id != nil && passwordName.MatchString(id.Name) {
			astutil.Report(pass, call.Pos(), "security-03",
				"%s hashes a password-shaped value; use bcrypt.GenerateFromPassword or argon2id", name)
		}
	}
})
```

**Exclusions:** MD5/SHA1 used for non-password purposes (checksums, ETags, cache keys) is
legitimate and common — the name gate on the argument is what keeps this rule narrow; do not
drop the gate to "any md5/sha1 call in the codebase."

**Finding.Message:** `"%s hashes a password-shaped value; use bcrypt.GenerateFromPassword or argon2id"`

**Finding.Severity:** `SeverityError` — password compromise on any offline breach of the
hash store; compliance-blocking.

---

### security-04 — `security.static_aead_nonce`

**Source sentence:** "AES-GCM for symmetric encryption, with a unique nonce per key, never
reused." (rule 4.)

**Checkable:** yes for the narrow, high-confidence case of a *literal* nonce — full
reuse-across-calls detection needs interprocedural dataflow and is excluded (see exclusions
below); a nonce passed as a composite literal is unambiguously static and always wrong.

**Predicate:** `*ast.CallExpr` where `astutil.IsMethodOn(pass, call, "crypto/cipher", "AEAD",
"Seal")` or `..."Open"` holds (replacing the former manual `*ast.SelectorExpr` unwrap +
`implementsAEAD` interface check), and the nonce argument (2nd positional arg for both `Seal`
and `Open`) is an `*ast.CompositeLit` (`[]byte{...}`) or a `*ast.BasicLit`.

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	var method string
	switch {
	case astutil.IsMethodOn(pass, call, "crypto/cipher", "AEAD", "Seal"):
		method = "Seal"
	case astutil.IsMethodOn(pass, call, "crypto/cipher", "AEAD", "Open"):
		method = "Open"
	default:
		return
	}
	if len(call.Args) < 2 {
		return
	}
	switch call.Args[1].(type) {
	case *ast.CompositeLit, *ast.BasicLit:
		astutil.Report(pass, call.Pos(), "security-04",
			"nonce passed to %s is a literal, not freshly generated; use crypto/rand per call", method)
	}
})
```

**Exclusions:** nonce sourced from a variable — even if that variable is itself statically
assigned once at package scope and reused across every call — is NOT caught here; that
requires tracking the variable's declaration and reference count across the package, which is
a broader dataflow problem. Flagging only the inline-literal case keeps false positives at
effectively zero (a literal nonce byte slice passed directly to `Seal`/`Open` has no
legitimate use).

**Finding.Message:** `"nonce passed to %s is a literal, not freshly generated; use crypto/rand per call"`

**Finding.Severity:** `SeverityError` — GCM nonce reuse under the same key allows full
plaintext and authentication-key recovery; this is a catastrophic, not incremental, crypto
break.

---

### security-05 — `security.timing_unsafe_secret_compare`

**Source sentence:** "`subtle.ConstantTimeCompare` for tokens, HMAC digests, API keys. `==`
leaks the match length through timing." (rule 5, Critical Trap #3.)

**Checkable:** yes, name-gated to bound false positives — `==`/`!=` on strings/byte slices is
extremely common and mostly benign; only flag when at least one operand's identifier matches
a secret-name regex.

**Predicate:** `*ast.BinaryExpr` with `Op == token.EQL || Op == token.NEQ`, where
`pass.TypesInfo.TypeOf` of either operand is `string` or `[]byte`, AND at least one operand's
base identifier matches `(?i)(token|secret|apikey|api_key|hmac|signature|mac|password)`.

```go
insp.Preorder([]ast.Node{(*ast.BinaryExpr)(nil)}, func(n ast.Node) {
	be := n.(*ast.BinaryExpr)
	if be.Op != token.EQL && be.Op != token.NEQ {
		return
	}
	if !isStringOrBytes(pass, be.X) || !isStringOrBytes(pass, be.Y) {
		return
	}
	if secretName(be.X) || secretName(be.Y) {
		astutil.Report(pass, be.Pos(), "security-05",
			"%s compares secret-shaped value with %s; use subtle.ConstantTimeCompare")
	}
})
```

**Exclusions:** comparisons against a compile-time empty-string sentinel (`x == ""`) are
presence checks, not secret comparisons — exclude when the other operand is a `*ast.BasicLit`
with value `""`.

**Finding.Message:** `"%s compares secret-shaped value with %s; use subtle.ConstantTimeCompare"`

**Finding.Severity:** `SeverityWarning` — real timing side-channel, but exploitation needs
many low-jitter network samples; less immediately catastrophic than key/plaintext recovery
(security-04), still must fix before ship.

---

### security-06 — `security.hardcoded_credential`

**Source sentence:** "Never embed credentials in source... Source is git history." (rule 6,
partial — source/error-message/log-field are three separate injection sites; this rule covers
source only, security-07 covers error/log.)

**Checkable:** yes — syntactic on the assignment shape, name-gated.

**Predicate:** `*ast.AssignStmt` or `*ast.ValueSpec` where the LHS/declared identifier name
matches `(?i)(password|secret|apikey|api_key|credential|privatekey)` AND the RHS is a
non-empty `*ast.BasicLit` of kind `STRING`.

```go
var credentialName = regexp.MustCompile(`(?i)(password|secret|apikey|api_key|credential|privatekey)`)

insp.Preorder([]ast.Node{(*ast.ValueSpec)(nil), (*ast.AssignStmt)(nil)}, func(n ast.Node) {
	switch s := n.(type) {
	case *ast.ValueSpec:
		for i, name := range s.Names {
			if credentialName.MatchString(name.Name) && isNonEmptyStringLit(s.Values, i) {
				astutil.Report(pass, name.Pos(), "security-06",
					"%s is a hardcoded credential-shaped literal; source secrets from env or a secrets manager", name.Name)
			}
		}
	case *ast.AssignStmt:
		for i, lhs := range s.Lhs {
			id, ok := lhs.(*ast.Ident)
			if ok && credentialName.MatchString(id.Name) && isNonEmptyStringLit(s.Rhs, i) {
				astutil.Report(pass, id.Pos(), "security-06",
					"%s is a hardcoded credential-shaped literal; source secrets from env or a secrets manager", id.Name)
			}
		}
	}
})
```

**Exclusions:** RHS sourced from `os.Getenv`/`os.LookupEnv`/a config-struct field/a secrets
client call is fine — those are `*ast.CallExpr`, not `*ast.BasicLit`, so they never match this
predicate; no extra exclusion logic needed. Test files (`_test.go`) commonly need literal
fixture credentials — exclude files matching `_test.go` suffix from this pass entirely.

**Finding.Message:** `"%s is a hardcoded credential-shaped literal; source secrets from env or a secrets manager"`

**Finding.Severity:** `SeverityError` — permanent compromise via git history; release-blocking.

---

### security-07 — `security.secret_in_log_or_error`

**Source sentence:** "Never embed credentials in source, error messages, or log fields."
(rule 6, error-message/log-field portion.) Also folds in rule 12's confidentiality angle where
the leaked value is itself secret material (the "never log AND return" duplicate-noise angle
of rule 12 is NOT security — see cross-reference list).

**Checkable:** yes, name-gated at the call site.

**Predicate:** `*ast.CallExpr` whose callee is one of `loggingFuncs` (the shared
slog/log/fmt.Print list) or `fmt.Errorf`, where any argument's base identifier matches
`secretField`.

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	var fn string
	switch {
	case astutil.IsPkgFunc(pass, call, "fmt", "Errorf"):
		fn = "fmt.Errorf"
	default:
		var ok bool
		fn, ok = matchedLoggingFunc(pass, call)
		if !ok {
			return
		}
	}
	for _, arg := range call.Args {
		id := baseIdent(arg)
		if id == nil || !secretField.MatchString(id.Name) {
			continue
		}
		if implementsStringerOrLogValuer(pass.TypesInfo.TypeOf(arg)) {
			continue
		}
		astutil.Report(pass, call.Pos(), "security-07",
			"%s (secret-shaped) passed to %s; strip or mask before logging/wrapping", id.Name, fn)
	}
})
```

**Exclusions:** an argument that is itself a *masked* type (implements `String()`+`LogValue()`
per security-01, checked here via the same shared `implementsStringerOrLogValuer` helper)
passed by value is safe to log — skip if both methods are present on its static type.

**Finding.Message:** `"%s (secret-shaped) passed to %s; strip or mask before logging/wrapping"`

**Finding.Severity:** `SeverityError` — direct path to secret exposure in the log aggregator
or a returned error surfaced to callers/telemetry; matches the source's production incident
verbatim (a named secret field reaching a log call is the exact failure, not a latent risk).

---

### security-08 — `security.whole_struct_logged`

**Source sentence:** "Log specific fields, never whole structs or objects. `%v`, unexported
fields, and `String()` fallthrough all leak more than intended." (rule 17.)

**Checkable:** yes — semantic on argument static type.

**Predicate:** `*ast.CallExpr` to a function in the shared `loggingFuncs` list where a
positional value argument's static type (`pass.TypesInfo.TypeOf(arg)`) is a named struct type
(`*types.Named` underlying `*types.Struct`) that does NOT implement `fmt.Stringer` or
`slog.LogValuer` (via `implementsStringerOrLogValuer`).

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	fn, ok := matchedLoggingFunc(pass, call)
	if !ok {
		return
	}
	for _, arg := range valuePositions(call) { // odd slots for slog k/v pairs; all args for fmt.Print*
		t := pass.TypesInfo.TypeOf(arg)
		named, ok := t.(*types.Named)
		if !ok {
			continue
		}
		if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
			continue
		}
		if implementsStringerOrLogValuer(named) {
			continue
		}
		astutil.Report(pass, arg.Pos(), "security-08",
			"%s value logged whole via %s; log specific fields instead", named.Obj().Name(), fn)
	}
})
```

**Exclusions:** structs with zero fields, or whose every field is already a scalar with an
obviously non-secret name — still flag (per recipe: prefer the rule as written over inventing
a second heuristic layer that itself risks missed leaks); the source's own production note is
precisely "a struct grew a secret field later and nobody updated the log call" — narrowing
this rule to "only if a field looks secret-shaped" defeats its purpose.

**Finding.Message:** `"%s value logged whole via %s; log specific fields instead"`

**Finding.Severity:** `SeverityWarning` — leak is latent (depends on the struct acquiring or
already having a secret field) rather than a confirmed named-secret sighting like
security-01/07; real risk, one severity step below those.

---

### security-09 — `security.sql_string_concat`

**Source sentence:** "Parameterized queries only. No concatenation for SQL or DynamoDB filter
expressions. Injection is a one-character bug." (rule 10, Critical Trap #5.)

**Checkable:** yes — fully syntactic, canonical injection pattern.

**Predicate:** `*ast.CallExpr` to one of `{*sql.DB, *sql.Tx, *sql.Conn} × {Query,
QueryContext, Exec, ExecContext, QueryRow, QueryRowContext}`, checked via `astutil.IsMethodOn`
per candidate instead of the former hand-rolled `isSQLExecMethod`, where the query-string
argument (first arg after context, or first arg) is a `*ast.BinaryExpr` with `Op ==
token.ADD`, or a `*ast.CallExpr` to `fmt.Sprintf`.

```go
var sqlExecMethods = []struct{ typeName, method string }{
	{"DB", "Query"}, {"DB", "QueryContext"}, {"DB", "Exec"}, {"DB", "ExecContext"}, {"DB", "QueryRow"}, {"DB", "QueryRowContext"},
	{"Tx", "Query"}, {"Tx", "QueryContext"}, {"Tx", "Exec"}, {"Tx", "ExecContext"}, {"Tx", "QueryRow"}, {"Tx", "QueryRowContext"},
	{"Conn", "Query"}, {"Conn", "QueryContext"}, {"Conn", "Exec"}, {"Conn", "ExecContext"}, {"Conn", "QueryRow"}, {"Conn", "QueryRowContext"},
}

func isSQLExecCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	for _, m := range sqlExecMethods {
		if astutil.IsMethodOn(pass, call, "database/sql", m.typeName, m.method) {
			return true
		}
	}
	return false
}
```

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	if !isSQLExecCall(pass, call) {
		return
	}
	q := queryArg(call) // resolves position accounting for optional leading ctx
	switch v := q.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			astutil.Report(pass, call.Pos(), "security-09",
				"query passed to %s is built via %s, not parameters; use placeholders (%s / $N) and pass args",
				"string concatenation")
		}
	case *ast.CallExpr:
		if astutil.IsPkgFunc(pass, v, "fmt", "Sprintf") {
			astutil.Report(pass, call.Pos(), "security-09",
				"query passed to %s is built via %s, not parameters; use placeholders (%s / $N) and pass args",
				"fmt.Sprintf")
		}
	}
})
```

**Exclusions:** none needed — a query string built by `+` or `Sprintf` immediately adjacent to
the exec call is never correct; this is the textbook zero-false-positive AST injection check.

**Finding.Message:** `"query passed to %s is built via %s, not parameters; use placeholders (%s / $N) and pass args"`

**Finding.Severity:** `SeverityError` — "injection is a one-character bug," per source; direct
compromise path.

---

### security-10 — `security.dynamodb_expression_concat`

**Source sentence:** same as security-09 — "No concatenation for SQL **or DynamoDB filter
expressions**." (rule 10.) Split into its own rule because the AST predicate targets a
different type (AWS SDK input struct field, not a `database/sql` method call).

**Checkable:** yes — semantic on the assigned struct field's type/name.

**Predicate:** `*ast.KeyValueExpr` or `*ast.AssignStmt` where the target is a field named
`FilterExpression`, `KeyConditionExpression`, `ConditionExpression`, or `UpdateExpression` on
a type from `github.com/aws/aws-sdk-go(-v2)/service/dynamodb` (resolved via
`pass.TypesInfo.TypeOf`), and the value expression — after unwrapping a single-arg
pointer-helper call (`aws.String`/`aws.Int64`/`aws.Int32`/`aws.Bool`/`aws.Float64`, the
idiomatic AWS SDK v2 wrapper since these fields are `*string`) — is a `*ast.BinaryExpr` with
`token.ADD` or a `fmt.Sprintf` call.

```go
var dynamoExprFields = map[string]bool{
	"FilterExpression": true, "KeyConditionExpression": true,
	"ConditionExpression": true, "UpdateExpression": true,
}

var awsPointerHelpers = []string{"String", "Int64", "Int32", "Bool", "Float64"}
// unwrapAWSPointerHelper returns call.Args[0] if e is a single-arg call to one of the
// AWS SDK v2 scalar-to-pointer helpers, otherwise returns e unchanged. Preserved verbatim in
// shape from the pre-conformance version — only its internal name match now goes through
// astutil.IsPkgFunc instead of the banned calleeName.
func unwrapAWSPointerHelper(pass *analysis.Pass, e ast.Expr) ast.Expr {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return e
	}
	for _, name := range awsPointerHelpers {
		if astutil.IsPkgFunc(pass, call, "github.com/aws/aws-sdk-go-v2/aws", name) {
			return call.Args[0]
		}
	}
	return e
}
```

```go
insp.Preorder([]ast.Node{(*ast.KeyValueExpr)(nil)}, func(n ast.Node) {
	kv := n.(*ast.KeyValueExpr)
	key, ok := kv.Key.(*ast.Ident)
	if !ok || !dynamoExprFields[key.Name] {
		return
	}
	if !isDynamoDBInputStruct(pass, enclosingCompositeLitType(n)) {
		return
	}
	switch v := unwrapAWSPointerHelper(pass, kv.Value).(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			astutil.Report(pass, kv.Pos(), "security-10",
				"%s built via string concatenation; use expression.NewBuilder or placeholders (:val)", key.Name)
		}
	case *ast.CallExpr:
		if astutil.IsPkgFunc(pass, v, "fmt", "Sprintf") {
			astutil.Report(pass, kv.Pos(), "security-10",
				"%s built via string concatenation; use expression.NewBuilder or placeholders (:val)", key.Name)
		}
	}
})
```

**Exclusions:** values built via the SDK's own `expression.Builder` (a `*ast.CallExpr` to
`expression.Expression.Build()` or similar) are the correct pattern — those never match
`*ast.BinaryExpr`/`fmt.Sprintf` (with or without the pointer-helper unwrap) and pass through
untouched.

**Finding.Message:** `"%s built via string concatenation; use expression.NewBuilder or placeholders (:val)"`

**Finding.Severity:** `SeverityError` — same injection class as security-09.

---

### security-11 — `security.regexp_dynamic_pattern`

**Source sentence:** "Bound or avoid regex on untrusted input to prevent ReDoS." (rule 9,
ReDoS clause.)

**Checkable:** partially — true ReDoS requires a catastrophic-backtracking pattern shape
*and* attacker-controlled input reaching it; neither is reliably decidable from syntax alone.
What IS cleanly checkable is the proxy signal "pattern string is not a literal," i.e., the
regex is assembled or passed in at runtime rather than fixed at compile time. This is a weak
signal (many dynamic patterns are safe, sourced from static config) — kept as a low-severity
nudge, not a hard rule, per the recipe's guidance to prefer excluding fragile heuristics; this
one is included because the source rule explicitly calls out regex-on-untrusted-input as a
named concern and the literal-vs-non-literal check has zero false negatives for the "compiled
straight from an HTTP param" case even though it also flags safe dynamic-but-static-config
patterns.

**Predicate:** `*ast.CallExpr` to `regexp.Compile` or `regexp.MustCompile` (checked via two
`astutil.IsPkgFunc` calls, replacing the former `typeutil.StaticCallee` + name-string switch)
where the pattern argument is not a `*ast.BasicLit`.

```go
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	var name string
	switch {
	case astutil.IsPkgFunc(pass, call, "regexp", "Compile"):
		name = "regexp.Compile"
	case astutil.IsPkgFunc(pass, call, "regexp", "MustCompile"):
		name = "regexp.MustCompile"
	default:
		return
	}
	if len(call.Args) != 1 {
		return
	}
	if _, isLit := call.Args[0].(*ast.BasicLit); !isLit {
		astutil.Report(pass, call.Pos(), "security-11",
			"regexp pattern passed to %s is not a literal; confirm it cannot be influenced by untrusted input", name)
	}
})
```

**Exclusions:** pattern built from a package-level `const` string (still not a `*ast.BasicLit`
at the call site, so this predicate cannot distinguish it from a request-derived pattern — the
severity is deliberately `SeverityInfo` specifically because of this unresolved false-positive
class; do not upgrade this rule's severity without adding const-propagation to the predicate).

**Finding.Message:** `"regexp pattern passed to %s is not a literal; confirm it cannot be influenced by untrusted input"`

**Finding.Severity:** `SeverityInfo` — proxy signal only, not a confirmed vulnerability;
review nudge given the source's explicit ReDoS callout, but the false-positive rate on static
non-literal patterns (config-sourced) is too high for `SeverityWarning` or above.

---

### security-12 — `security.pii_in_span_attribute`

**Source sentence:** "Keep PII and secrets out of span attributes, the same rule as logging."
(rule 16, PII clause — the "create spans for meaningful work" and "propagate trace context"
clauses of the same source rule are observability, not security; see cross-reference list.)

**Checkable:** yes — same name-gate pattern as security-07, applied to OTel attribute-setting
calls instead of log calls.

**Predicate:** `*ast.CallExpr` to `(trace.Span).SetAttributes` — checked via
`astutil.IsMethodOn(pass, call, "go.opentelemetry.io/otel/trace", "Span", "SetAttributes")`,
replacing the former manual `*ast.SelectorExpr` unwrap + `isOtelSpan` receiver check — where a
key argument (extracted via `astutil.StringLit`, replacing the banned `unquote`) matches the
secret/PII regex (`(?i)(secret|password|token|apikey|credential|email|ssn)`).

```go
var piiOrSecretKey = regexp.MustCompile(`(?i)(secret|password|token|apikey|credential|email|ssn)`)

insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	if !astutil.IsMethodOn(pass, call, "go.opentelemetry.io/otel/trace", "Span", "SetAttributes") {
		return
	}
	for _, arg := range call.Args {
		kv, ok := arg.(*ast.CallExpr) // attribute.String("key", val)
		if !ok || len(kv.Args) == 0 {
			continue
		}
		key, ok := astutil.StringLit(kv.Args[0])
		if !ok || !piiOrSecretKey.MatchString(key) {
			continue
		}
		astutil.Report(pass, call.Pos(), "security-12",
			"span attribute %q looks PII/secret-shaped; remove or hash before attaching", key)
	}
})
```

**Exclusions:** none beyond the name gate — attribute key strings are almost always literals
at the call site in idiomatic OTel usage, so this predicate has low false-negative risk and the
name gate keeps false positives low.

**Finding.Message:** `"span attribute %q looks PII/secret-shaped; remove or hash before attaching"`

**Finding.Severity:** `SeverityWarning` — real leak surface (traces export to a backend), but
typically lower exposure/retention window than persistent log aggregation (security-07/08);
still must fix.

---

### security-13 — `security.integer_narrowing_conversion`

**Provenance:** not present in the source document; this analyzer-specific extension is
modeled on gosec's G115 rule (integer overflow/truncation from an unchecked type conversion).
No source-document sentence to quote; the rationale is this analyzer's own: narrowing a
runtime-computed integer via an explicit conversion silently wraps or truncates the value
instead of erroring, and that class of bug is exactly what gosec added G115 to catch.

**Checkable:** yes, syntactically and semantically — explicit conversions are `*ast.CallExpr`
with a type identifier as `Fun`; source/target types and constant-ness are all `go/types`
facts, no dataflow needed. What is NOT checkable from local AST/type facts alone: whether the
value was already bounds-checked by an `if` a few lines above the conversion. Detecting that
reliably needs path-sensitive analysis (which branch dominates the conversion, what range was
actually checked) — explicitly OUT OF SCOPE for v0.1.0 rather than specified as an exclusion
this analyzer cannot implement. This rule is a best-effort shape-matcher, not a proof of a bug,
hence `SeverityWarning` rather than `SeverityError`.

**Predicate:** `*ast.CallExpr` where `Fun` denotes a type (`pass.TypesInfo.Types[call.Fun].IsType()`)
resolving to a predeclared narrow integer type (`int8`/`int16`/`int32`, `uint8`/`uint16`/`uint32`,
and their aliases `byte`/`rune`) — a `*types.Named` result (a user-defined integer type such as
`type MyInt32 int32`) is deliberately excluded, since narrowing to an intentionally-defined
domain type is a different judgment call than narrowing to a raw builtin. The single argument's
static type (`pass.TypesInfo.TypeOf`) must be a wider integer type by platform-accurate size
(`pass.TypesSizes.Sizeof`, not a hardcoded bit-width table — this is exactly the case
`contracts.md`'s `NeedTypesSizes` requirement exists for), and the argument must NOT be a
compile-time constant (`pass.TypesInfo.Types[arg].Value != nil` catches both literals and
named-constant identifiers — the compiler already range-checks those, so flagging them is
noise).

```go
// integerConversionTarget reports the target *types.Basic if t is a predeclared narrow
// integer type this rule cares about (int8/16/32, uint8/16/32, byte, rune), or ok=false
// otherwise. A *types.Named result (a user-defined integer type) is deliberately excluded.
func integerConversionTarget(t types.Type) (b *types.Basic, ok bool) {
	if _, named := t.(*types.Named); named {
		return nil, false
	}
	basic, ok := t.(*types.Basic)
	if !ok || basic.Info()&types.IsInteger == 0 {
		return nil, false
	}
	switch basic.Kind() {
	case types.Int8, types.Int16, types.Int32,
		types.Uint8, types.Uint16, types.Uint32:
		return basic, true
	default:
		return nil, false
	}
}
insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
	call := n.(*ast.CallExpr)
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok || !tv.IsType() || len(call.Args) != 1 {
		return // not a single-argument type conversion
	}
	target, ok := integerConversionTarget(tv.Type)
	if !ok {
		return
	}
	arg := call.Args[0]
	if pass.TypesInfo.Types[arg].Value != nil {
		return // compile-time constant; compiler already range-checks it
	}
	srcType := pass.TypesInfo.TypeOf(arg)
	srcBasic, ok := srcType.Underlying().(*types.Basic)
	if !ok || srcBasic.Info()&types.IsInteger == 0 {
		return
	}
	if pass.TypesSizes.Sizeof(srcType) <= pass.TypesSizes.Sizeof(tv.Type) {
		return // not narrowing
	}
	astutil.Report(pass, call.Pos(), "security-13",
		"conversion narrows %s to %s with no visible bounds check (gosec G115-style)", srcType, target)
})
```

**Exclusions:** the "preceded by an explicit bounds check" exclusion from gosec's own design is
explicitly OUT OF SCOPE for v0.1.0 — reliably detecting "was this value range-checked on every
path that reaches this conversion" is path-sensitive dataflow, not a local AST/type fact, and
specifying an exclusion this analyzer cannot actually implement would misrepresent what the tool
does; documented here as a known false-positive source instead of a fake guarantee. User-defined
named integer target types are excluded (see predicate). Untyped/compile-time-constant source
expressions are excluded (see predicate).

**Finding.Message:** `"conversion narrows %s to %s with no visible bounds check (gosec G115-style)"`

**Finding.Severity:** `SeverityWarning` — heuristic shape-match, not a proof of overflow; cannot
distinguish an already-checked value from an unchecked one, so this is a review nudge, not a
hard error.

---

### security-14 — `security.unclosed_http_response_body`

**Provenance:** not present in the source document; this analyzer-specific extension covers a
`*http.Response.Body` is an `io.ReadCloser` backed by the underlying TCP connection; leaving it
unclosed prevents `net/http`'s transport from returning the connection to its pool, which under
sustained load exhausts file descriptors / pooled connections — a real production
connection-pool-exhaustion outage pattern, not a theoretical concern.

**Checkable:** yes, with an explicit ceiling — this predicate is a same-function, whole-body
scan (`ast.Inspect` over the enclosing `*ast.FuncDecl`/`*ast.FuncLit`), not full
interprocedural/escape analysis. If the response (or its `Body`) is passed to another function
that closes it there, or the response variable name is shadowed/reused within the same
function, this predicate cannot see that and may misjudge it — stated explicitly as a v0.1.0
ceiling, not silently assumed away, matching security-02's own name-based (not full-dataflow)
gate philosophy.

**Predicate:** `*ast.CallExpr` matching one of the 8 known response-returning call shapes
(checked via `astutil.IsPkgFunc`/`astutil.IsMethodOn`, the established candidate-list pattern
from `weakHashFuncs`/`sqlExecMethods`), whose result flows (via `enclosingAssignTarget`) into a
local identifier name. If that name's `.Body` is accessed anywhere later in the same function
AND no `<name>.Body.Close()` call (bare or `defer`) exists anywhere in the same function,
report. A response that is never used to access `.Body` at all (fully discarded, e.g.
`resp, _ := http.Get(url)` with `resp` never touched again) does NOT trigger this rule — that is
a different smell (ignoring the response/error entirely), explicitly out of scope for this rule,
which is specifically about a body that IS read but never closed. The idiomatic
check-error-then-`defer resp.Body.Close()` pattern is not a special case in this predicate: as
long as a `Close()` call on `.Body` exists anywhere in the function, the finding is suppressed
regardless of which branch it's in, so the standard error-checked pattern is silent by
construction, not by an added exclusion.

```go
var httpResponseCallers = []struct {
	pkg, recv, name string // recv == "" for a package-level func
}{
	{"net/http", "", "Get"},
	{"net/http", "", "Post"},
	{"net/http", "", "Head"},
	{"net/http", "", "PostForm"},
	{"net/http", "Client", "Do"},
	{"net/http", "Client", "Get"},
	{"net/http", "Client", "Post"},
	{"net/http", "Client", "Head"},
}

// matchedHTTPResponseCall reports whether call matches one of httpResponseCallers.
func matchedHTTPResponseCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	for _, c := range httpResponseCallers {
		if c.recv == "" {
			if astutil.IsPkgFunc(pass, call, c.pkg, c.name) {
				return true
			}
			continue
		}
		if astutil.IsMethodOn(pass, call, c.pkg, c.recv, c.name) {
			return true
		}
	}
	return false
}

insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}, func(n ast.Node) {
	var body *ast.BlockStmt
	switch fn := n.(type) {
	case *ast.FuncDecl:
		body = fn.Body
	case *ast.FuncLit:
		body = fn.Body
	}
	if body == nil {
		return
	}

	type respSite struct {
		name string
		pos  token.Pos
	}
	var responses []respSite
	bodyAccessed := map[string]bool{}
	bodyClosed := map[string]bool{}

	ast.Inspect(body, func(m ast.Node) bool {
		switch v := m.(type) {
		case *ast.CallExpr:
			if matchedHTTPResponseCall(pass, v) {
				if name := enclosingAssignTarget(v); name != "" {
					responses = append(responses, respSite{name, v.Pos()})
				}
				return true
			}
			// resp.Body.Close() or defer resp.Body.Close()
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
				if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Body" {
					if id, ok := inner.X.(*ast.Ident); ok {
						bodyClosed[id.Name] = true
					}
				}
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "Body" {
				if id, ok := v.X.(*ast.Ident); ok {
					bodyAccessed[id.Name] = true
				}
			}
		}
		return true
	})

	for _, r := range responses {
		if bodyAccessed[r.name] && !bodyClosed[r.name] {
			astutil.Report(pass, r.pos, "security-14",
				"%s.Body is read but never closed in this function; leaks the pooled connection", r.name)
		}
	}
})
```

**Exclusions:** a response whose `.Body` is never accessed at all is explicitly OUT OF SCOPE —
decided, not left ambiguous: this rule is about a body that is read but not closed, not about a
discarded/unused response, which is a distinct (unhandled-error-shaped) smell. Variable-name
reuse/shadowing within the same function collapses into one name-keyed bucket, a known
limitation of this v0.1.0 name-based heuristic. Passing the response or its body to another
function for closing elsewhere is not tracked (same-function ceiling, see Checkable).

**Finding.Message:** `"%s.Body is read but never closed in this function; leaks the pooled connection"`

**Finding.Severity:** `SeverityError` — an unclosed response body leaks the underlying TCP
connection; the transport's connection pool cannot reuse or release it, and under load this
exhausts file descriptors / pooled connections, a real production outage pattern.

---

## Excluded — not reducible to an AST predicate with acceptable false-positive risk

| Source rule | Why excluded |
|---|---|
| "Never roll custom crypto" (rule 3) | Detecting "custom crypto" requires recognizing homemade cipher/hash logic by its *absence* of a stdlib/vetted-library call — no positive syntactic signature exists; any heuristic (e.g., flag XOR-in-a-loop) is trivially defeated and noisy on legitimate non-crypto code. |
| "Validate all input at system boundaries" (rule 8, general clause) | "Boundary" and "validated" are architectural/semantic judgments (which functions are ingress, what counts as validation) — not resolvable per-node without a whole-program call-graph model of trust boundaries. |
| Cap body size / limit JSON depth & array length (rule 9, first two sub-clauses) | Absence-of-a-guard detection; whether a guard is required depends on framework/middleware that may apply it elsewhere (e.g., a shared HTTP middleware already wraps `r.Body`) — high false-positive rate without cross-file config awareness. |
| Path traversal — `filepath.Clean` + rooted-prefix check (rule 9, path clause) | `filepath.Join` already calls `Clean` internally, so the real gap (missing rooted-prefix check against an absolute base) is a taint-tracking problem: is the path argument attacker-controlled, and was it checked *anywhere* upstream in the call chain. Not resolvable from local AST/type facts alone. |
| SSRF — validate redirect/outbound targets (rule 9, SSRF clause) | Requires tracking whether a URL argument to an outbound HTTP call derives from request-controlled input across function boundaries — interprocedural dataflow, out of scope for a single-package `go/analysis` pass. |
| Dependency attack surface: `govulncheck`, `go mod verify`, pin/review new deps (rule 11) | CI/process-level check against `go.sum`/vuln DB, not a Go-source AST predicate; already covered by CI contract step in `contracts.md` (`golangci-lint`/CI pipeline), not this analyzer. |
| Secrets sourcing (env/secrets-manager), rotation schedule, KMS/envelope encryption, no baked-in-image (rule 7) | Deployment/infra practice (Dockerfile, Terraform, secrets-manager config) — outside Go source entirely. |
| Audit-log security-relevant state changes (rule 19) | Positive requirement to add logging at auth/permission/config-change sites — detecting the *absence* of an audit-log call at every such site needs a curated list of "security-relevant" call sites per codebase; not a generic syntactic pattern. |

8 exclusions.

## Cross-referenced — covered by the observability pass or another domain, not duplicated

| Source rule | Target domain | One-line disambiguator |
|---|---|---|
| "Never log AND return the same error" (rule 12) | `errors` domain | Alert-noise/duplication concern, not confidentiality — "is this a leak risk" is security's question, this is neither leak nor debuggability, it's error-handling hygiene. |
| "`log/slog` for all structured logging, never `fmt.Printf`" (rule 13) | `observability` | "Can an operator debug from logs" (queryability) — not a leak-risk question. |
| "Configure slog once at startup, `JSONHandler`/`TextHandler`, sampling" (rule 14) | `observability` | Logger configuration/operability, not what data is exposed. |
| "Always pass context to the logger (`slog.InfoContext`)" (rule 15) | `observability` | Trace correlation for debugging, not data exposure. |
| "Create OTel spans around meaningful work units; propagate trace context" (rule 16, non-PII clause) | `observability` | Span creation and context propagation are debuggability concerns; the PII/secret clause of the same source rule is security-12 above. |
| "Prometheus RED/USE metrics; label by stable dimensions, never user/request ID" (rule 18) | `observability` | Framed by the source as a cardinality/cost failure (series-count explosion), not confidentiality — though a user-ID label is mildly PII-adjacent, the dominant failure mode described is operational, so this stays with observability. |
| "Separate liveness from readiness" (rule 20) | `observability` | Pure availability/operability concern, no data-exposure dimension — but note: `observability-08` explicitly **excludes** this rule (no reliable static AST signal, see `phase-4a-observability.md` §2). Rule 20 is therefore encoded nowhere in this analyzer suite. Listed here as "not security's concern" rather than "implemented elsewhere" — a deliberate scope-out shared by both files, not a coverage gap owned by either. |

7 cross-references (6 to `observability`, 1 to `errors`). Of the 6 to `observability`, 5 map to an
implemented rule (`observability-01/02/03/05/07`); the 6th (rule 20) maps to a rule excluded in
both files, per the note above.

## 3. Fixture file spec

Per `contracts.md`'s "Testdata fixture layout — one isolated package per rule": one Go
package per rule, `internal/tools/testdata/fixtures/audit-security/rule<NN>/`, zero-padded to 2
digits, package `rule<NN>` (matching the directory name, per `phase-4a-errors.md`/`phase-4a-performance.md`'s
precedent for this same conformance pass — not the earlier flat `securityfixture` package this
file previously used, and not `naming.md`'s `package fixture` convention: two siblings already
converted under this exact pass agree on `package rule<NN>`, so that convention wins here rather
than blending both). Each rule directory holds exactly one `violation.go` and, where a
false-positive risk exists, one `compliant.go` — never a mixed file, so a stray finding can never
be misattributed to a sibling rule. `// VIOLATION: security-NN` / `// COMPLIANT: security-NN`
markers sit directly above the relevant line — the rule ID, not the dotted slug, per
`contracts.md`'s marker convention. No sensitive identifier-shaped literal appears anywhere
below.

No fixture pair below shares a same-package symbol across `violation.go`/`compliant.go` (unlike
`phase-4a-errors.md`'s rule03), so no file declares a helper solely for its sibling to consume.

**Resolved dependency/predicate notes, carried forward unchanged in substance:** security-10's
predicate resolves the struct type against `github.com/aws/aws-sdk-go-v2/service/dynamodb`;
security-12's against `go.opentelemetry.io/otel/trace.Span`. Both packages are covered by
`contracts.md`'s fixture-only dependency exception (`aws-sdk-go-v2/service/dynamodb`,
`aws-sdk-go-v2/aws`, `otel/trace`, `otel/attribute` as test-only `go.mod` requirements, never
imported from `internal/` production code). The real `dynamodb.ScanInput.FilterExpression`
field type is `*string`, so idiomatic code wraps the concatenation in `aws.String(...)`; §2's
`unwrapAWSPointerHelper` unwraps this single-arg pointer-helper call before its type switch, so
`rule10/violation.go` below expresses the realistic wrapped idiom and correctly triggers the
finding — this is fully resolved, not a documented gap, and §5's `TestAuditSecurity_Rule10`
below asserts the finding fires on exactly that fixture.

### Rule → file map

| Rule ID | File(s) | Violation case | Near-miss case |
|---|---|---|---|
| `security-01` | `rule01/violation.go`, `rule01/compliant.go` | `UnmaskedCredentials` (type) | `MaskedCredentials` (type, has `String`+`LogValue`) |
| `security-02` | `rule02/violation.go`, `rule02/violation_v2.go`, `rule02/compliant.go` | `GenerateSessionToken` (math/rand), `GenerateSessionTokenV2` (math/rand/v2) | `RetryBackoffJitter` |
| `security-03` | `rule03/violation.go`, `rule03/compliant.go` | `HashPassword` | `ChecksumPayload` |
| `security-04` | `rule04/violation.go`, `rule04/compliant.go` | `sealWithStaticNonce` | `sealWithGeneratedNonce` |
| `security-05` | `rule05/violation.go`, `rule05/compliant.go` | `ValidateAPIKeyUnsafe` | `HasAPIKey` |
| `security-06` | `rule06/violation.go`, `rule06/compliant.go` | `hardcodedAPIKey` (var) | `LoadAPIKeyFromEnv` |
| `security-07` | `rule07/violation.go`, `rule07/compliant.go` | `LogAuthFailureUnsafe` | `LogAuthFailureSafe` |
| `security-08` | `rule08/violation.go`, `rule08/compliant.go` | `LogUserWhole` | `LogUserSummaryWhole` |
| `security-09` | `rule09/violation.go`, `rule09/compliant.go` | `QueryUserUnsafe` | `QueryUserSafe` |
| `security-10` | `rule10/violation.go`, `rule10/compliant.go` | `BuildFilterExpressionUnsafe` | `BuildFilterExpressionSafe` |
| `security-11` | `rule11/violation.go`, `rule11/compliant.go` | `CompileDynamicPattern` | `CompileStaticPattern` |
| `security-12` | `rule12/violation.go`, `rule12/compliant.go` | `RecordJobSpanUnsafe` | `RecordJobSpanSafe` |
| `security-13` | `rule13/violation.go`, `rule13/compliant.go` | `TruncateOffset` | `TruncateOffsetChecked` |
| `security-14` | `rule14/violation.go`, `rule14/compliant.go` | `FetchStatusUnsafe` | `FetchStatusSafe` |

29 fixture files total: 14 rules × 2 files (`violation.go` + `compliant.go`), plus one extra
`rule02/violation_v2.go` covering the `math/rand/v2` import path added by the security-02
predicate fix (see security-02's row: `security-02`'s file list is `rule02/violation.go`,
`rule02/violation_v2.go`, `rule02/compliant.go`).

### File contents

**`rule01/violation.go`** (security-01)
```go
package rule01

// VIOLATION: security-01
type UnmaskedCredentials struct {
	APIKey string
	UserID string
}
```

**`rule01/compliant.go`**
```go
package rule01

import "log/slog"

// COMPLIANT: security-01
type MaskedCredentials struct {
	APIKey string
	UserID string
}

func (c MaskedCredentials) String() string {
	return "MaskedCredentials{APIKey: ****}"
}

func (c MaskedCredentials) LogValue() slog.Value {
	return slog.StringValue("MaskedCredentials{APIKey: ****}")
}
```

**`rule02/violation.go`** (security-02)
```go
package rule02

import mrand "math/rand"

func GenerateSessionToken() int64 {
	// VIOLATION: security-02
	sessionToken := mrand.Int63()
	return sessionToken
}
```

**`rule02/compliant.go`**
```go
package rule02

import mrand "math/rand"

func RetryBackoffJitter() int {
	// COMPLIANT: security-02
	jitterMS := mrand.Intn(100)
	return jitterMS
}
```

**`rule02/violation_v2.go`** (security-02, `math/rand/v2` import path)
```go
package rule02

import mrand "math/rand/v2"

func GenerateSessionTokenV2() int64 {
	// VIOLATION: security-02
	sessionToken := mrand.Int64()
	return sessionToken
}
```

**`rule03/violation.go`** (security-03)
```go
package rule03

import "crypto/sha256"

func HashPassword(password string) [32]byte {
	// VIOLATION: security-03
	return sha256.Sum256([]byte(password))
}
```

**`rule03/compliant.go`**
```go
package rule03

import "crypto/sha256"

func ChecksumPayload(payload string) [32]byte {
	// COMPLIANT: security-03
	return sha256.Sum256([]byte(payload))
}
```

**`rule04/violation.go`** (security-04)
```go
package rule04

import "crypto/cipher"

func sealWithStaticNonce(gcm cipher.AEAD, plaintext []byte) []byte {
	// VIOLATION: security-04
	return gcm.Seal(nil, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, plaintext, nil)
}
```

**`rule04/compliant.go`**
```go
package rule04

import (
	"crypto/cipher"
	"crypto/rand"
	"io"
)

func sealWithGeneratedNonce(gcm cipher.AEAD, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// COMPLIANT: security-04
	return gcm.Seal(nil, nonce, plaintext, nil), nil
}
```

**`rule05/violation.go`** (security-05)
```go
package rule05

func ValidateAPIKeyUnsafe(apiKey, storedKey string) bool {
	// VIOLATION: security-05
	return apiKey == storedKey
}
```

**`rule05/compliant.go`**
```go
package rule05

func HasAPIKey(apiKey string) bool {
	// COMPLIANT: security-05
	return apiKey == ""
}
```

**`rule06/violation.go`** (security-06)
```go
package rule06

// VIOLATION: security-06
var hardcodedAPIKey = "sk_live_abc123xyz"
```

**`rule06/compliant.go`**
```go
package rule06

import "os"

func LoadAPIKeyFromEnv() string {
	// COMPLIANT: security-06
	apiKey := os.Getenv("API_KEY")
	return apiKey
}
```

**`rule07/violation.go`** (security-07)
```go
package rule07

import "log/slog"

func LogAuthFailureUnsafe(apiKey string) {
	// VIOLATION: security-07
	slog.Error("auth failed", "apiKey", apiKey)
}
```

**`rule07/compliant.go`**
```go
package rule07

import "log/slog"

type SafeCredential struct {
	apiKey string
}

func (c SafeCredential) String() string { return "SafeCredential{****}" }

func (c SafeCredential) LogValue() slog.Value { return slog.StringValue("SafeCredential{****}") }

func LogAuthFailureSafe(credential SafeCredential) {
	// COMPLIANT: security-07
	slog.Error("auth failed", "credential", credential)
}
```

Near-miss exercises the masked-type exclusion (§2 security-07 exclusions): argument identifier
`credential` matches `secretField`, but its static type implements both `String()` and
`LogValue()`, so it must NOT trigger.

**`rule08/violation.go`** (security-08)
```go
package rule08

import "log/slog"

type UserRecord struct {
	ID    string
	Email string
}

func LogUserWhole(user UserRecord) {
	// VIOLATION: security-08
	slog.Info("user created", "user", user)
}
```

**`rule08/compliant.go`**
```go
package rule08

import "log/slog"

type UserSummary struct {
	ID string
}

func (u UserSummary) String() string { return "UserSummary{" + u.ID + "}" }

func LogUserSummaryWhole(summary UserSummary) {
	// COMPLIANT: security-08
	slog.Info("user created", "summary", summary)
}
```

**`rule09/violation.go`** (security-09)
```go
package rule09

import "database/sql"

func QueryUserUnsafe(db *sql.DB, userID string) (*sql.Rows, error) {
	// VIOLATION: security-09
	return db.Query("SELECT * FROM users WHERE id = " + userID)
}
```

**`rule09/compliant.go`**
```go
package rule09

import "database/sql"

func QueryUserSafe(db *sql.DB, userID string) (*sql.Rows, error) {
	// COMPLIANT: security-09
	return db.Query("SELECT * FROM users WHERE id = ?", userID)
}
```

**`rule10/violation.go`** (security-10) — uses the real `aws-sdk-go-v2/service/dynamodb` type
per the fixture-only dependency exception above. The violation wraps its concatenation in
`aws.String(...)`, matching the idiomatic AWS SDK v2 shape — §2's `unwrapAWSPointerHelper` sees
through this wrapper to the underlying `*ast.BinaryExpr` and still fires.
```go
package rule10

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func BuildFilterExpressionUnsafe(status string) *dynamodb.ScanInput {
	return &dynamodb.ScanInput{
		TableName: aws.String("jobs"),
		// VIOLATION: security-10
		FilterExpression: aws.String("status = " + status),
	}
}
```

**`rule10/compliant.go`**
```go
package rule10

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func BuildFilterExpressionSafe(status string) *dynamodb.ScanInput {
	return &dynamodb.ScanInput{
		TableName: aws.String("jobs"),
		// COMPLIANT: security-10
		FilterExpression: aws.String("#s = :status"),
	}
}
```

**`rule11/violation.go`** (security-11)
```go
package rule11

import "regexp"

func CompileDynamicPattern(pattern string) *regexp.Regexp {
	// VIOLATION: security-11
	return regexp.MustCompile(pattern)
}
```

**`rule11/compliant.go`**
```go
package rule11

import "regexp"

func CompileStaticPattern() *regexp.Regexp {
	// COMPLIANT: security-11
	return regexp.MustCompile(`^[a-z]+$`)
}
```

**`rule12/violation.go`** (security-12)
```go
package rule12

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func RecordJobSpanUnsafe(span trace.Span, token string) {
	// VIOLATION: security-12
	span.SetAttributes(attribute.String("api_token", token))
}
```

**`rule12/compliant.go`**
```go
package rule12

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func RecordJobSpanSafe(span trace.Span, jobID string) {
	// COMPLIANT: security-12
	span.SetAttributes(attribute.String("job_id", jobID))
}
```

**`rule13/violation.go`** (security-13)
```go
package rule13

func TruncateOffset(offset int64) int32 {
	// VIOLATION: security-13
	return int32(offset)
}
```

**`rule13/compliant.go`** — exercises the only exclusion this predicate actually implements
(compile-time-constant source), not the unimplementable bounds-check exclusion.
```go
package rule13

const maxOffset = 100

func TruncateOffsetChecked() int32 {
	// COMPLIANT: security-13
	return int32(maxOffset)
}
```

**`rule14/violation.go`** (security-14)
```go
package rule14

import (
	"io"
	"net/http"
)

func FetchStatusUnsafe(url string) ([]byte, error) {
	// VIOLATION: security-14
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}
```

**`rule14/compliant.go`**
```go
package rule14

import (
	"io"
	"net/http"
)

func FetchStatusSafe(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	// COMPLIANT: security-14
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
```

## 4. Tool file spec — `internal/tools/go_audit_security.go`

Reproduced verbatim from `contracts.md`'s "Conformance block — copy verbatim into every
domain file's §4," `<domain>`→`security`, `<Domain>`→`Security`.

```go
package tools

import (
	"context"
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/security"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditSecurityInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditSecurityOutput struct {
	Result finding.AuditResult `json:"result"`
}

func AuditSecurityHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditSecurityInput) (*mcp.CallToolResult, AuditSecurityOutput, error) {
	if err := normalizeAuditSecurityInput(&in); err != nil {
		return nil, AuditSecurityOutput{}, fmt.Errorf("validating input: %w", err)
	}
	ws, err := resolveInWorkspace(in.Package)
	if err != nil {
		return nil, AuditSecurityOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{security.Analyzer})
	if err != nil {
		return nil, AuditSecurityOutput{}, fmt.Errorf("running security audit: %w", err)
	}
	return nil, AuditSecurityOutput{Result: result}, nil
}

func RegisterAuditSecurity(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_audit_security",
		Description: "Audits a Go package for secret leakage, weak crypto, injection, and PII exposure (14 rules) and returns structured findings.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, AuditSecurityHandler)
}

func normalizeAuditSecurityInput(in *AuditSecurityInput) error {
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

`resolveInWorkspace` is defined once, project-wide (never redeclared here). `security.Analyzer`
is the single exported `*analysis.Analyzer` from `internal/analysis/security/` (§2's rules,
`package security`) — `audit.Run` is the sole caller of `checker.Analyze`; this file never calls
`packages.Load`, `checker.Analyze`, or `pass.Run` directly, and never redeclares
`runSecurityPass`, `runInspectDependency`, `inspectAnalyzer`, or a manual `analysis.Pass{}`
struct literal — all three are deleted, not replaced by an equivalent, since `audit.Run` now
owns that entire load/drive/collect loop. There is likewise no `recover()`/`defer` block in this
handler: `errors-10`/`errors-15` (panic-in-library-code / recover-at-boundary) are themselves
*audited properties of the scanned codebase*, not a wrapper this tool's own handler needs —
`checker.Analyze`'s own driver is the boundary, and this domain file does not re-derive one.
`ToolAnnotations` above are the read-only-audit defaults; `go_audit_security` performs no
writes, so no override is needed.

Not cached — matches `contracts.md`'s cache TTL table row "every test/analysis tool ... 0
(never cached)": `go_audit_security` is never wrapped in the `TTLCache`, no cache key computed.

### Analyzer identity and rule→predicate wiring

`internal/analysis/security/security.go` defines `Analyzer = &analysis.Analyzer{Name:
"security", ...}` per the canonical skeleton. `run(pass)` dispatches by node type; each rule's
predicate from section 2 becomes one `insp.Preorder` block (inline, per rule, as shown above)
or a small named checker function where the predicate is reused verbatim in the wiring table
below:

| Node type in `Preorder` | Rule(s) dispatched | Checker |
|---|---|---|
| `*ast.TypeSpec` | security-01 | `checkUnmaskedSecretStruct` |
| `*ast.CallExpr` | security-02, 03, 04, 07, 08, 09, 11, 12 | `checkMathRandSecretToken`, `checkWeakPasswordHash`, `checkStaticAEADNonce`, `checkSecretInLogOrError`, `checkWholeStructLogged`, `checkSQLStringConcat`, `checkRegexpDynamicPattern`, `checkPIIInSpanAttribute` |
| `*ast.BinaryExpr` | security-05 | `checkTimingUnsafeSecretCompare` |
| `*ast.AssignStmt`, `*ast.ValueSpec` | security-06 | `checkHardcodedCredential` |
| `*ast.KeyValueExpr` | security-10 | `checkDynamoDBExpressionConcat` |

Each checker takes `(pass *analysis.Pass, n ast.Node)` and implements exactly the
predicate/exclusion pair specified for its rule in section 2 — no additional heuristics.
`astutil.Report(pass, pos, "security-NN", tmpl, args...)` builds the `Finding`: `Rule` is the
`security-NN` ID passed at the call site, `RuleName` and `Severity` are resolved from that ID
via `astutil.RuleName`/`astutil.RuleSeverity` against the `init()` registration (panicking on a
typo'd/unregistered rule ID, by design), and `Location` is `pass.Fset.Position(pos)`. No domain
file constructs a `Finding` by hand.

## 5. Verification spec

One `TestAuditSecurity_Rule<NN>` + one `TestAuditSecurity_Rule<NN>_CompliantIsSilent` per rule
(28 tests) plus one `TestAuditSecurity_TotalRuleCount`, in
`internal/analysis/security/security_test.go` (package `security_test`) — these exercise
`security.Analyzer` directly via `astutil.RunFixture`, so they live beside the analyzer, not
behind the MCP tool handler in `internal/tools/`. No mega `TestAuditSecurity` with `t.Run`
subtests, no `findingsForRule`/`mustRunSecurity` helper (both banned). Every fixture reference
uses `astutil.RunFixture(t, security.Analyzer, "audit-security/rule<NN>")`; every `Location.File`
assertion checks the real workspace-relative path (`fixtures/audit-security/rule<NN>/...`),
never `filepath.Base` and never an "at that line" prose claim.

**Note on `_CompliantIsSilent`:** `contracts.md`'s literal §5 template runs the fixture once
and asserts no returned `Finding` anywhere carries the rule's own ID. For every rule here,
`violation.go` and `compliant.go` share one fixture package/directory, and the genuine
violation-file finding — asserted to exist by the sibling `TestAuditSecurity_Rule<NN>` test —
*does* carry that exact rule ID; an unscoped "no finding anywhere has this Rule" assertion over
the same fixture load cannot pass alongside it. Resolved the same way `phase-4a-naming.md`'s actual
`_CompliantIsSilent` tests resolve it (not CONTRACTS' literal template, and not
`phase-4a-errors.md`/`phase-4a-performance.md`'s unfixed copies of that template): scope the assertion to
findings whose `Location.File` matches the compliant fixture file specifically —
`fixtures/audit-security/rule<NN>/compliant.go` — before asserting `NotEqual`.

Fully worked example (`security-01`):

```go
func TestAuditSecurity_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule01")

	require.Len(t, findings, 1, "expected exactly one security-01 finding")
	f := findings[0]
	assert.Equal(t, "security-01", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-security/rule01/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line) // `type UnmaskedCredentials struct {`
}

func TestAuditSecurity_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule01")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-security/rule01/compliant.go" {
			assert.NotEqual(t, "security-01", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditSecurity_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("security"), 14) // catches a rule silently dropped or added
}
```

`security-02` is a partial exception to the "applied identically" pattern below: this pass's fix
gave its fixture package a second violation file (`rule02/violation_v2.go`, covering the
`math/rand/v2` import path — see §3), so `RunFixture` over `audit-security/rule02` now returns two
findings, one per import path, instead of the generic one:

```go
func TestAuditSecurity_Rule02(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule02")

	require.Len(t, findings, 2, "expected one security-02 finding per math/rand import path")
	byFile := map[string]finding.Finding{}
	for _, f := range findings {
		assert.Equal(t, "security-02", f.Rule)
		assert.Equal(t, finding.SeverityError, f.Severity)
		byFile[f.Location.File] = f
	}

	v1, ok := byFile["fixtures/audit-security/rule02/violation.go"]
	require.True(t, ok, "expected a finding in violation.go (math/rand)")
	assert.Equal(t, 7, v1.Location.Line) // `sessionToken := mrand.Int63()`

	v2, ok := byFile["fixtures/audit-security/rule02/violation_v2.go"]
	require.True(t, ok, "expected a finding in violation_v2.go (math/rand/v2)")
	assert.Equal(t, 7, v2.Location.Line) // `sessionToken := mrand.Int64()`
}

func TestAuditSecurity_Rule02_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule02")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-security/rule02/compliant.go" {
			assert.NotEqual(t, "security-02", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
```

Applied identically for `security-03` through `security-12`, each pair asserting (a) exactly one
`Finding` with that `Rule` ID at the fixture's real violating line, and (b) `compliant.go`'s
near-miss never contributes a finding for that rule, scoped as above:

| Rule | `Location.Line` (violation.go) | Line content |
|---|---|---|
| `security-03` | 7 | `return sha256.Sum256([]byte(password))` |
| `security-04` | 7 | `return gcm.Seal(nil, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, plaintext, nil)` |
| `security-05` | 5 | `return apiKey == storedKey` |
| `security-06` | 4 | `var hardcodedAPIKey = "sk_live_abc123xyz"` |
| `security-07` | 7 | `slog.Error("auth failed", "apiKey", apiKey)` |
| `security-08` | 12 | `slog.Info("user created", "user", user)` |
| `security-09` | 7 | `return db.Query("SELECT * FROM users WHERE id = " + userID)` |
| `security-10` | 12 | `FilterExpression: aws.String("status = " + status),` |
| `security-11` | 7 | `return regexp.MustCompile(pattern)` |
| `security-12` | 10 | `span.SetAttributes(attribute.String("api_token", token))` |

`security-10`'s test additionally confirms the resolved `aws.String(...)` unwrap: the finding
fires at `rule10/violation.go:12` (the `FilterExpression` key-value line), not zero findings —
this is the regression check for the gap that a stale earlier draft of this file had flagged as
unresolved. No rule above carries a documented open gap; every predicate's true-positive fixture
fires exactly once.

`security-13` and `security-14` are pass-added (see §2) and follow the same one-`Finding`,
scoped-`_CompliantIsSilent` shape as `security-03` through `security-12` above, spelled out
individually rather than folded into the table since each closes out a distinct new predicate:

```go
func TestAuditSecurity_Rule13(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule13")

	require.Len(t, findings, 1, "expected exactly one security-13 finding")
	f := findings[0]
	assert.Equal(t, "security-13", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-security/rule13/violation.go", f.Location.File)
	assert.Equal(t, 5, f.Location.Line) // `return int32(offset)`
}

func TestAuditSecurity_Rule13_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule13")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-security/rule13/compliant.go" {
			assert.NotEqual(t, "security-13", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditSecurity_Rule14(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule14")

	require.Len(t, findings, 1, "expected exactly one security-14 finding")
	f := findings[0]
	assert.Equal(t, "security-14", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-security/rule14/violation.go", f.Location.File)
	assert.Equal(t, 14, f.Location.Line) // `return io.ReadAll(resp.Body)` — the read site with no preceding Close
}

func TestAuditSecurity_Rule14_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, security.Analyzer, "audit-security/rule14")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-security/rule14/compliant.go" {
			assert.NotEqual(t, "security-14", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
```

<!-- SECTIONS 3-5 COMPLETE: fixtures, tool, verification specified -->
