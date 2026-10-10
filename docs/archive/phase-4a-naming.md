# Domain 5 — Naming (`go_audit_naming`)

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

The original 16-rule naming corpus is reproduced in this specification and is
not shared with another domain file. Extends
`Finding`/`AuditResult`/`Severity`/`Location` and the pass skeleton from `contracts.md`.
Does not redefine them. Several numbered rules bundle 2-3 independently-decidable
imperatives — split into atomic rows below per `phase-4a-index.md`'s "distinctness is about
content, not source line number" test. Two mechanically-identical sub-checks (package-name
charset, package-name-blacklist reason buckets) are merged into one `Finding.Rule` with a
`%s`-interpolated reason, mirroring `observability-01`'s banned-call-list precedent in
`phase-4a-observability.md`.

## Shared helpers

Package `naming` (`internal/analysis/naming/naming.go`) — one subpackage per domain, per
`contracts.md`'s "Naming and file layout" section (never `package analysis`, which
self-collides with importing `golang.org/x/tools/go/analysis`; this file previously documented
the old flat-file `package analysis` model — corrected here, not by the original review).

```go
package naming

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
	astutil.RegisterRule("naming-01", "identifier-charset-casing", finding.SeverityInfo)
	astutil.RegisterRule("naming-02", "inconsistent-acronym-casing", finding.SeverityWarning)
	astutil.RegisterRule("naming-03", "type-name-in-identifier", finding.SeverityInfo)
	astutil.RegisterRule("naming-04", "shadows-builtin", finding.SeverityWarning)
	astutil.RegisterRule("naming-05", "shadows-stdlib-package", finding.SeverityWarning)
	astutil.RegisterRule("naming-08", "package-name-charset-casing", finding.SeverityWarning)
	astutil.RegisterRule("naming-09", "package-name-banned", finding.SeverityWarning)
	astutil.RegisterRule("naming-11", "file-name-casing", finding.SeverityInfo)
	astutil.RegisterRule("naming-13", "package-name-stutter", finding.SeverityWarning)
	astutil.RegisterRule("naming-14", "method-receiver-stutter", finding.SeverityWarning)
	astutil.RegisterRule("naming-15", "receiver-banned-name", finding.SeverityWarning)
	astutil.RegisterRule("naming-16", "receiver-too-long", finding.SeverityInfo)
	astutil.RegisterRule("naming-17", "receiver-name-inconsistent", finding.SeverityWarning)
	astutil.RegisterRule("naming-18", "getter-get-prefix", finding.SeverityWarning)
	astutil.RegisterRule("naming-20", "interface-missing-er-suffix", finding.SeverityInfo)
	astutil.RegisterRule("naming-21", "interface-literal-name", finding.SeverityWarning)
	astutil.RegisterRule("naming-24", "enum-missing-type-prefix", finding.SeverityInfo)
	astutil.RegisterRule("naming-26", "sentinel-error-missing-prefix", finding.SeverityWarning)
	astutil.RegisterRule("naming-27", "error-type-missing-suffix", finding.SeverityWarning)
	// naming-23 retired — see its §2 entry: superseded by typedesign-08.
}

var Analyzer = &analysis.Analyzer{
	Name:     "naming",
	Doc:      "checks Go naming conventions: casing, acronyms, stutter, receivers, getters, interfaces, enums, sentinels",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}
```

`run(pass *analysis.Pass) (interface{}, error)` dispatches to each rule's own
`insp.Preorder`/`insp.WithStack`/`pass.Files` block shown in its own §2 entry below, in rule-ID
order, ending `return nil, nil` per the canonical pass skeleton — not reproduced as one
mega-function here, to avoid duplicating content already shown per-rule.

`isQualifiedCall` (naming-03's conversion-exception check, formerly reused from `errors.go`) is
replaced by `astutil.IsPkgFunc` per `contracts.md`'s conformance block, not redeclared
locally. `stringLitValue` and `enclosingFuncDecl` (also formerly claimed as "reused, not
redefined" from `errors.go`) are dropped from that claim entirely — neither is actually invoked
anywhere in this file's own predicates. `isTestFile` (naming-01's exclusion) **is** genuinely
used here but isn't part of `astutil`'s declared exported surface, so it's a small local
unexported helper, not a cross-domain import. `isErrorType`/`isFmtErrorfOrNew` (naming-26's
sentinel-detection predicate, formerly claimed as "reused... from `errors.go`") are likewise not
part of `astutil`'s declared surface — `phase-4a-errors.md` documents its own copy of the same shape
for a different rule; each domain package gets a small local copy rather than a cross-package
import of an unexported symbol from a sibling `internal/analysis/errors` package:

```go
// isTestFile reports whether pos falls inside a _test.go file.
func isTestFile(pass *analysis.Pass, pos token.Pos) bool {
	return strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go")
}

// isErrorType reports whether name's declared type is exactly the built-in error interface.
func isErrorType(pass *analysis.Pass, name *ast.Ident) bool {
	obj := pass.TypesInfo.Defs[name]
	return obj != nil && types.Identical(obj.Type(), types.Universe.Lookup("error").Type())
}

// isFmtErrorfOrNew reports whether call is errors.New(...) or fmt.Errorf(...).
func isFmtErrorfOrNew(pass *analysis.Pass, call *ast.CallExpr) (pkg, name string, ok bool) {
	if astutil.IsPkgFunc(pass, call, "errors", "New") {
		return "errors", "New", true
	}
	if astutil.IsPkgFunc(pass, call, "fmt", "Errorf") {
		return "fmt", "Errorf", true
	}
	return "", "", false
}

// identifierCaseReason reports why name violates charset/casing convention, or ok=false
// if it's clean. Checks non-ASCII runes before snake_case so the more specific reason wins.
var snakeRe = regexp.MustCompile(`[a-zA-Z0-9]_[a-zA-Z0-9]`)

func identifierCaseReason(name string) (reason string, bad bool) {
	if name == "" || name == "_" {
		return "", false
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return "contains non-ASCII characters", true
		}
	}
	if snakeRe.MatchString(name) {
		return "uses snake_case or SCREAMING_SNAKE_CASE", true
	}
	return "", false
}

// knownAcronyms is the closed set this pass checks for consistent casing. Not exhaustive —
// narrows to acronyms this codebase's own domain (automation, HTTP, storage) actually uses,
// keeping the false-positive surface small.
var knownAcronyms = []string{"API", "ID", "URL", "HTTP", "XML", "JSON", "SQL", "UUID", "CPU", "IO"}

// titleCasedAcronym reports the acronym found mis-cased as Title-case (e.g. "Api" inside
// "ApiKey") inside name, or ok=false if none of knownAcronyms appears in that shape.
func titleCasedAcronym(name string) (acronym string, bad bool) {
	for _, ac := range knownAcronyms {
		titleCased := string(ac[0]) + strings.ToLower(ac[1:])
		if strings.Contains(name, titleCased) {
			return ac, true
		}
	}
	return "", false
}

// shadowsBuiltin reports whether name resolves to a predeclared (universe-scope) identifier
// — go/types, not a hardcoded list, so it catches every builtin (min, max, len, clear, copy,
// new, make, error, cap, ...) without drifting out of sync with the language.
func shadowsBuiltin(name string) bool {
	return name != "_" && types.Universe.Lookup(name) != nil
}

// stdlibPackageNames, catchAllPackageNames, reservedDirNames back packageNameBanReason.
var stdlibPackageNames = map[string]bool{
	"json": true, "log": true, "user": true, "mail": true, "csv": true,
	"path": true, "regexp": true, "time": true, "url": true,
}
var catchAllPackageNames = map[string]bool{
	"util": true, "helper": true, "common": true, "types": true, "interfaces": true,
}
var reservedDirNames = map[string]bool{"vendor": true, "testdata": true, "internal": true}

// packageNameBanReason reports why a package clause name is banned, or ok=false if clean.
func packageNameBanReason(name string) (reason string, bad bool) {
	switch {
	case catchAllPackageNames[name]:
		return "catch-all dumping-ground name with no domain boundary", true
	case reservedDirNames[name]:
		return "reserved directory-semantic name, not a package identity", true
	case stdlibPackageNames[name]:
		return "clashes with a stdlib package name", true
	default:
		return "", false
	}
}

// wordBoundaryAfter reports whether s[idx] starts a new PascalCase word — either idx is at
// end of string, or s[idx] is uppercase. Used to confirm a stutter/suffix match isn't a false
// substring hit mid-word (e.g. "Statement" must not match a "State" prefix check).
func wordBoundaryAfter(s string, idx int) bool {
	return idx >= len(s) || (s[idx] >= 'A' && s[idx] <= 'Z')
}
```

Rule-specific helpers too narrow for reuse are shown inline in that rule's predicate section.

---

## 1. Rules table

| Rule ID | Source rule | Status | Severity |
|---|---|---|---|
| `naming-01` | 1 — charset/casing: non-ASCII, snake_case, SCREAMING_SNAKE_CASE | Implemented | Info |
| `naming-02` | 2 — inconsistent acronym casing (`ApiKey` vs `APIKey`) | Implemented | Warning |
| `naming-03` | 3 — type name baked into identifier (`countInt`) | Implemented | Info |
| `naming-04` | 4 (partial) — shadows a predeclared builtin | Implemented | Warning |
| `naming-05` | 4 (partial) — shadows a stdlib package name | Implemented | Warning |
| `naming-06` | 5 — length tracks scope | **Excluded** | — |
| `naming-07` | 6 — export only what callers need | **Excluded** | — |
| `naming-08` | 7 (partial) — package name charset/casing (uppercase, underscore, non-ASCII) | Implemented | Warning |
| `naming-09` | 7 (partial) — package name catch-all / reserved-dir / stdlib-clash | Implemented | Warning |
| `naming-10` | 7 (partial) — plurals, abbreviation clarity | **Excluded** | — |
| `naming-11` | 8 (partial) — file name has uppercase letters or hyphens | Implemented | Info |
| `naming-12` | 8 (partial) — cross-file separator consistency, build-suffix misuse | **Excluded** | — |
| `naming-13` | 9 — package-name stutter in exported symbol | Implemented | Warning |
| `naming-14` | 10 — method name stutters its receiver type | Implemented | Warning |
| `naming-15` | 11 (partial) — receiver named `this`/`self`/`me` | Implemented | Warning |
| `naming-16` | 11 (partial) — receiver longer than 3 characters | Implemented | Info |
| `naming-17` | 11 (partial) — inconsistent receiver name across one type's methods | Implemented | Warning |
| `naming-18` | 12 (partial) — getter uses `Get` prefix | Implemented | Warning |
| `naming-19` | 12 (partial) — `Set` prefix / `Is`/`Has`/`Can` enforcement | **Excluded** | — |
| `naming-20` | 13 (partial) — single-method interface missing `-er`/`-r` suffix | Implemented | Info |
| `naming-21` | 13 (partial) — interface name contains literal `Interface` | Implemented | Warning |
| `naming-22` | 13 (partial) — multi-method interface "descriptive noun" | **Excluded** | — |
| `naming-24` | 14 (partial) — enum constant missing type-name prefix | Implemented | Info |
| `naming-25` | 15 — constants named by role, not value | **Excluded** | — |
| `naming-26` | 16 (partial) — sentinel error var missing `Err`/`err` prefix | Implemented | Warning |
| `naming-27` | 16 (partial) — custom error type missing `Error` suffix | Implemented | Warning |
| `naming-28` | 16 (partial) — error string casing/punctuation/acronyms | **Excluded** (cross-ref) | — |

19 implemented, 8 excluded, 1 retired (naming-23, cross-domain duplicate of `typedesign-08` — see its §2 entry below). Naming correctly has no Error-severity rule after retirement; do not add one to compensate. Numbering is domain-local and sequential, matching every other
Phase 4 file's convention (`phase-4a-errors.md`, `phase-4a-observability.md`).

---

## 2. Per-rule AST pattern

### naming-01 — charset and casing

**Source:** "Unicode letters, digits, underscores only... Never snake_case,
SCREAMING_SNAKE_CASE, or all-caps. Stick to ASCII."

**Checkable:** yes, for the two clauses with zero judgment: an identifier containing a
non-ASCII rune, or one matching the snake_case shape (`letter_or_digit` `_` `letter_or_digit`).
The clauses "no Go keywords," "cannot start with a digit," and "Unicode letters/digits/
underscores only [as opposed to symbols]" are dropped — code violating any of those doesn't
parse, so the compiler already rejects it before this pass ever runs; encoding them here would
be dead code guarding against inputs that can't exist.

**Predicate** (`*ast.Ident`, any declaring position — `*ast.ValueSpec.Names`,
`*ast.Field.Names`, `*ast.FuncDecl.Name`, `*ast.TypeSpec.Name`, `*ast.AssignStmt` LHS with `:=`):

```go
insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
	id := n.(*ast.Ident)
	if pass.TypesInfo.Defs[id] == nil {
		return // only flag declaring occurrences, not every reference
	}
	if reason, bad := identifierCaseReason(id.Name); bad {
		astutil.Report(pass, id.Pos(), "naming-01", "identifier %q %s", id.Name, reason)
	}
})
```

**Exclusions:** non-declaring occurrences (`pass.TypesInfo.Defs[id] == nil`) — otherwise every
read of a badly-named identifier would be reported once per reference instead of once at
declaration; test subcase names inside `t.Run("Foo_Bar", ...)` string literals are not
`*ast.Ident` at all, so they're structurally out of scope already; `_test.go` `TestFoo_Bar`
function names are `*ast.Ident` but the underscore sits between two capital-letter words with no
digit/letter directly adjacent on both sides per Go's own test-subcase convention documented in
`go-testing.md` — `snakeRe` still matches `Foo_Bar` structurally, so add an explicit
`isTestFile(pass, id.Pos()) && strings.HasPrefix(id.Name, "Test")` guard before reporting.

**Finding.Message:** `"identifier %q %s"` (name, reason)

**Finding.Severity:** Info. Zero behavioral or compile-time effect — pure convention, same tier
as `errors-07`'s error-string-casing rule in `phase-4a-errors.md`.

---

### naming-02 — inconsistent acronym casing

**Source:** "Case an acronym the same way throughout the identifier: `apiKey` or `APIKey`,
never `ApiKey`. `userID` not `userId`, `xml` or `XML`, never `Xml`."

**Checkable:** yes, against a closed, curated acronym list (`knownAcronyms`) — checking every
theoretically-possible acronym is unbounded and would misfire on ordinary words ("Api" as a
false hit inside an unrelated word is the risk this list-based approach avoids by staying
narrow), but the given examples plus this project's own domain vocabulary are a safe,
high-precision starting set.

**Predicate** (`*ast.Ident`, declaring positions only):

```go
insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
	id := n.(*ast.Ident)
	if pass.TypesInfo.Defs[id] == nil {
		return
	}
	if ac, bad := titleCasedAcronym(id.Name); bad {
		astutil.Report(pass, id.Pos(), "naming-02", "identifier %q casing %s inconsistently, use all-upper or all-lower throughout", id.Name, ac)
	}
})
```

**Exclusions:** only the Title-cased shape (`Api`, `Id`, `Xml`) is flagged — `API`, `api`,
`ID`, `id`, `XML`, `xml` are all structurally excluded by `titleCasedAcronym`'s exact-string
match against `strings.ToLower`-tail form; a word that merely contains the same three letters
in a different casing pattern by coincidence isn't checked for, since `knownAcronyms` requires
the exact Title-case rendering, not a fuzzy match.

**Finding.Message:** `"identifier %q casing %s inconsistently, use all-upper or all-lower throughout"` (name, acronym)

**Finding.Severity:** Warning. Two spellings of the same concept (`ApiKey` next to `APIKey`
elsewhere) break grep/rename tooling and read as two different things during review — the
exact "generic name is a latent merge conflict" failure mode called out in this reference's own
Production Note, just via casing instead of a generic word.

---

### naming-03 — type name baked into the identifier

**Source:** "`count` not `countInt`, `amount` not `float64Amount`, `results` not
`resultSlice`. Exception: when converting between types, append the target type to
distinguish, `userIDStr := strconv.Itoa(userID)`."

**Checkable:** yes, for a closed suffix→type table, resolved via `go/types` so it isn't a bare
string-suffix guess — `countInt` only fires because `count`'s declared type actually is `int`,
not because the text "Int" appears.

**Predicate** (`*ast.ValueSpec` / `*ast.AssignStmt` LHS `*ast.Ident` with a resolvable type):

```go
var typeNameSuffixes = map[string]string{
	"Int": "int", "Bool": "bool", "Str": "string", "Float64": "float64", "Float32": "float32",
}

func redundantTypeSuffix(pass *analysis.Pass, id *ast.Ident, rhs ast.Expr) (suffix string, bad bool) {
	t := pass.TypesInfo.TypeOf(rhs)
	if t == nil {
		return "", false
	}
	for suf, want := range typeNameSuffixes {
		if strings.HasSuffix(id.Name, suf) && len(id.Name) > len(suf) && t.String() == want {
			return suf, true
		}
	}
	return "", false
}
```

**Exclusions:** the explicit conversion exception — skip when `rhs` is a `*ast.CallExpr` to a
conversion function (`astutil.IsPkgFunc(pass, call, "strconv", "Itoa")` and siblings
`FormatInt`/`FormatFloat`/`Quote`, or a builtin type-conversion `*ast.CallExpr` whose `Fun` is
an `*ast.Ident` naming a basic type) — that's the source's own sanctioned pattern
(`userIDStr := strconv.Itoa(userID)`), not the violation it looks like at the AST-shape level.

**Report call** — the predicate above only resolves `redundantTypeSuffix`; the missing wiring
(the original doc defined the helper but never called `report`, leaving naming-03 undocumented
as non-functional despite §1 marking it "Implemented") is completed here so the rule actually
fires, using this rule's own already-documented message template:

```go
insp.Preorder([]ast.Node{(*ast.AssignStmt)(nil)}, func(n ast.Node) {
	as := n.(*ast.AssignStmt)
	if as.Tok != token.DEFINE || len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		if suf, bad := redundantTypeSuffix(pass, id, as.Rhs[i]); bad {
			astutil.Report(pass, id.Pos(), "naming-03", "identifier %q encodes its own type %q in the name, drop the %q suffix", id.Name, typeNameSuffixes[suf], suf)
		}
	}
})
```

**Finding.Message:** `"identifier %q encodes its own type %q in the name, drop the %q suffix"` (name, type, suffix)

**Finding.Severity:** Info. Pure readability nit — no behavior or API-contract implication.

---

### naming-04 — shadows a predeclared builtin

**Source:** "Do not shadow `int`, `bool`, `any`, `min`, `max`, `len`, `clear`, `copy`, `new`,
`make`, `error`, `cap`."

**Checkable:** yes, exactly — `types.Universe.Lookup` is the authoritative, exhaustive source
for "is this name predeclared," no hardcoded list to fall out of sync with future Go builtins
(e.g. `min`/`max`/`clear` were added in 1.21/1.21/1.21 respectively; a hardcoded list froze at
the source doc's writing would miss the next one, `go/types` never will).

**Predicate** (`*ast.Ident` at a declaring position — param, local `:=`, named return, receiver):

```go
insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
	id := n.(*ast.Ident)
	if pass.TypesInfo.Defs[id] == nil {
		return
	}
	if shadowsBuiltin(id.Name) {
		astutil.Report(pass, id.Pos(), "naming-04", "%q shadows the predeclared identifier of the same name", id.Name)
	}
})
```

**Exclusions:** package-level `Defs` for a legitimately-exported symbol with a colliding short
name (rare, but e.g. a domain type genuinely named `Error` implementing a specific interface)
is still flagged — no carve-out, because the risk (masking the builtin inside that entire
file/package scope) is identical regardless of export status; the only structural exclusion is
`_` (blank identifier, explicitly permitted, never a shadow).

**Finding.Message:** `"%q shadows the predeclared identifier of the same name"` (name)

**Finding.Severity:** Warning. Doesn't always break something (many shadows are locally inert),
but a shadowed `error`/`len`/`new` inside a large function body silently changes what a later
line in the same scope resolves to — real debugging-session risk, not pure style.

---

### naming-05 — shadows a stdlib package name

**Source:** "Avoid clashing with stdlib package names: `json`, `log`, `user`, `mail`, `csv`,
`path`, `regexp`, `time`, `url`."

**Checkable:** yes — same mechanism family as `naming-04`, different lookup table
(`stdlibPackageNames`, a fixed list since "stdlib package name" isn't a `go/types` universe
concept the way builtins are).

**Predicate** (`*ast.Ident` at a declaring position, restricted to local/param scope — package-
level declarations named e.g. `time` inside `package time` itself are a non-issue and excluded
by construction since this pass runs per-package, never on the stdlib itself):

```go
insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
	id := n.(*ast.Ident)
	obj := pass.TypesInfo.Defs[id]
	if obj == nil || obj.Parent() == pass.Pkg.Scope() {
		return // package-level decls aren't "shadowing" a package import, they ARE the identity
	}
	if stdlibPackageNames[id.Name] {
		astutil.Report(pass, id.Pos(), "naming-05", "local declaration %q shadows the stdlib package %q imported in this file", id.Name, id.Name)
	}
})
```

**Exclusions:** package-level declarations (`obj.Parent() == pass.Pkg.Scope()`) — a
package-level `var log Logger` isn't shadowing anything mid-function, it's this package's own
top-level symbol, checked instead by `naming-01`/`naming-02`'s casing rules, not this one; only
function-local (`:=`, params, named returns) declarations that shadow an *already-imported*
package in the same file are the actual failure mode this rule targets.

**Finding.Message:** `"local declaration %q shadows the stdlib package %q imported in this file"` (name, name)

**Finding.Severity:** Warning. The compiler catches the sharpest form (using the package after
the shadow fails to compile), but the shadow forces an awkward late rename mid-review and blocks
using that package for the rest of the shadowed scope — friction, not correctness risk, hence
one tier below `naming-04`'s builtin-shadow (which can silently change resolved behavior instead
of just failing to compile).

---

### naming-06 — EXCLUDED: length tracks scope

**Source:** "The primary heuristic: the further a name is from its declaration, the more
descriptive it must be. Short loop or range blocks under 10 lines: 1-2 letters are fine...
Function-local vars used in several places: `count`, `sum`, `retries`. Package-level, struct
fields, exported symbols: fully descriptive."

**Why excluded, not encoded as a heuristic:** the source itself frames this as "the primary
heuristic," not a rule with a bright line — the only concrete number given ("under 10 lines")
applies to one narrow case (loop/range blocks), with no equivalent numeric threshold for
"function-local, used in several places" or "package-level." Any single global LOC threshold
applied uniformly across all three tiers either fires constantly on legitimate long-but-simple
functions or misses genuinely too-short names in tight scopes — there's no AST predicate here,
only a proxy for the same qualitative judgment `phase-4a-observability.md`'s `observability-06`
excludes for "meaningful work unit." Not encoded.

---

### naming-07 — EXCLUDED: export only what callers need

**Source:** "Default to unexported. Capitalize only when a consumer needs the symbol... In
`package main`, keep everything unexported unless reflection accesses it (JSON struct fields).
Write shy code, reveal nothing unnecessary."

**Why excluded, not encoded as a heuristic:** "does a consumer need this" requires
whole-module call-graph knowledge this single-package pass doesn't have (an exported symbol
with zero in-repo callers today may be this package's public contract for an external module —
four owned services import each other's client packages per `contracts.md`'s architecture,
so "unused within this package" is not "unused"). It also requires reflection-surface awareness
— an exported struct field with no direct code reference may exist solely for `encoding/json`
marshaling, which a syntactic/`go/types` pass cannot distinguish from a genuinely dead export
without parsing struct tags and correlating against every marshal call site in the program. Not
encoded; same false-positive risk class as `observability-06`'s span-attribute exclusion.

---

### naming-08 — package name charset and casing

**Source:** "Lowercase ASCII letters and digits only. No underscores, no plurals, no
MixedCaps... Do not start a package name with `.` or `_`."

**Checkable:** yes, for charset/casing (uppercase letters, underscores, non-ASCII) — a leading
`.`/`_` is structurally covered by the same "no underscores" check plus a leading-dot check;
Go's grammar already forbids a package clause starting with `.`, so only the leading-`_` case
is live.

**Predicate** (`*ast.File.Name`, once per file):

```go
insp.Preorder([]ast.Node{(*ast.File)(nil)}, func(n ast.Node) {
	f := n.(*ast.File)
	name := f.Name.Name
	switch {
	case strings.ContainsAny(name, "_"):
		astutil.Report(pass, f.Name.Pos(), "naming-08", "package name %q %s", name, "contains an underscore")
	case name != strings.ToLower(name):
		astutil.Report(pass, f.Name.Pos(), "naming-08", "package name %q %s", name, "contains uppercase letters")
	default:
		for _, r := range name {
			if r > unicode.MaxASCII {
				astutil.Report(pass, f.Name.Pos(), "naming-08", "package name %q %s", name, "contains non-ASCII characters")
				break
			}
		}
	}
})
```

**Exclusions:** none — every file in a package declares the identical `package` clause, so this
fires once per file, which is expected (matches how every other file-scoped check in this
project's passes runs, e.g. `observability-01`'s per-call-site reporting) and is cheap to
dedupe downstream if a caller wants one finding per package instead of per file.

**Finding.Message:** `"package name %q %s"` (name, reason)

**Finding.Severity:** Warning. A malformed package name is visible in every import statement
across every consumer forever — wider blast radius than an internal identifier, but still a
naming convention, not a defect, hence Warning not Error.

---

### naming-09 — package name catch-all, reserved, or stdlib-clashing

**Source:** "Never `util`, `helper`, `common`, `types`, `interfaces`... Avoid names that clash
with stdlib (`json`, `mail`, `log`, `url`)... `vendor`, `testdata`, `internal` are reserved
directory names with special semantics, not package names."

**Checkable:** yes — three closed-list exact-match checks, merged into one rule since the
detection mechanism is identical for all three (literal lookup against a blacklist), matching
`observability-01`'s precedent of one `Finding.Rule` covering a for-loop over several banned
targets.

**Predicate** (`*ast.File.Name`, once per file):

```go
insp.Preorder([]ast.Node{(*ast.File)(nil)}, func(n ast.Node) {
	f := n.(*ast.File)
	if reason, bad := packageNameBanReason(f.Name.Name); bad {
		astutil.Report(pass, f.Name.Pos(), "naming-09", "package name %q %s", f.Name.Name, reason)
	}
})
```

**Exclusions:** none needed — all three sub-lists are exact package-clause-name matches with
no ambiguity (a package genuinely named `log` unambiguously clashes with `log`, regardless of
what it does).

**Finding.Message:** `"package name %q %s"` (name, reason)

**Finding.Severity:** Warning. The catch-all case (`util`/`helper`/`common`) is the dominant
real-world risk this rule guards against — an unbounded dumping ground that "no one owns" per
the reference's own Trap #3 — significant enough to flag loudly, but still a structural/design
smell rather than a compile or runtime defect, so Warning not Error; the reserved-dir and
stdlib-clash cases share the same severity for mechanical consistency within one `Finding.Rule`.

---

### naming-10 — EXCLUDED: plurals, abbreviation clarity

**Source:** "Short, one-word nouns preferred... no plurals... Abbreviate only when the meaning
stays clear: `strconv`, `expvar`."

**Why excluded, not encoded as a heuristic:** reliable plural detection needs a
lemmatizer/dictionary, not a suffix regex — a naive `strings.HasSuffix(name, "s")` check
misfires on ordinary non-plural nouns that happen to end in "s" (`status`, `address`, `process`,
`focus`, `business`), which are common, legitimate Go package names; the false-positive rate on
real codebases would be high enough to make the finding noise, not signal. "Abbreviate only
when meaning stays clear" is a pure readability judgment call with no syntactic signal at all —
`strconv` is judged clear only by convention and long stdlib precedent, not by any structural
property distinguishing it from an unclear abbreviation of the same length. Not encoded.

---

### naming-11 — file name has uppercase letters or hyphens

**Source:** "Ideal: one lowercase word summarizing the file's contents... Multi-word:
concatenate without separator or use underscores, but pick one and stay consistent."

**Checkable:** yes, for the unambiguous negative case — any uppercase letter or hyphen in a
`.go` filename is never correct Go convention regardless of which separator style (concatenated
vs underscored) the file picks.

**Predicate** (checked once per file via `pass.Fset`, not an AST node — the base filename):

```go
for _, f := range pass.Files {
	base := filepath.Base(pass.Fset.Position(f.Pos()).Filename)
	if strings.ContainsAny(base, "-") || base != strings.ToLower(base) {
		astutil.Report(pass, f.Pos(), "naming-11", "file name %q should be lowercase with no hyphens", base)
	}
}
```

**Exclusions:** none for the uppercase/hyphen check itself — it's unconditionally wrong in Go
filename convention. The separate "pick one [separator style] and stay consistent across the
package" clause and the special-suffix-misuse clause (`_test.go`, `_darwin.go`, `_amd64.go` used
for something other than their documented meaning) are **not** covered here — see `naming-12`.

**Finding.Message:** `"file name %q should be lowercase with no hyphens"` (basename)

**Finding.Severity:** Info. Cosmetic — doesn't affect build, tooling, or readability enough to
outrank a functional naming defect.

---

### naming-12 — EXCLUDED: cross-file separator consistency, build-suffix misuse

**Source:** "Multi-word: concatenate without separator (`routingindex.go`) or use underscores
(`routing_index.go`), but pick one and stay consistent. Special suffixes have meaning, do not
use them unless you mean to: `_test.go` (test only), `_darwin.go`/`_linux.go`/`_windows.go`
(OS-specific), `_amd64.go`/`_arm64.go` (arch-specific)."

**Why excluded, not encoded as a heuristic:** "stay consistent" is a package-wide majority-vote
judgment (is `snake_case.go` the package's established style, or the one-off exception? a static
pass can count but can't decide which style is "the" convention without a threshold that's
itself arbitrary). Detecting suffix *misuse* (`_darwin.go` on a file with no darwin-specific
logic) requires correlating the filename against the file's actual build constraints and content
— a file legitimately named `..._darwin.go` with darwin-only code is correct; the same suffix
on a file that happens to also compile fine elsewhere isn't detectable from the name or a single
file's AST alone, it's a project-structure/build-tag cross-check outside this pass's scope. Not
encoded.

---

### naming-13 — package-name stutter in exported symbol

**Source:** "Do not repeat the package name in exported symbols, the package is already the
namespace. `worker.New()` not `worker.NewWorker()`, `worker.Jobs()` not
`worker.WorkerJobs()`, `worker.Config` not `worker.WorkerConfig`... Exception:
when the type name matches the package name (`worker.Worker`, `time.Time`,
`context.Context`), the repetition is hard to avoid without making one side less clear."

**Checkable:** yes — package-level exported `*ast.FuncDecl`/`*ast.TypeSpec`/`*ast.GenDecl` (var,
const) name whose text starts with the package's own name (case-insensitive prefix, confirmed at
a PascalCase word boundary so `Customs` doesn't false-match a `Custom` prefix check that isn't
even relevant here — the check is exact-string prefix, not fuzzy).

**Predicate** (`*ast.FuncDecl` shown; `*ast.TypeSpec` follows the identical shape):

```go
insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv != nil || !fn.Name.IsExported() {
		return // methods are naming-14's rule, not this one
	}
	pkg := pass.Pkg.Name()
	titlePkg := strings.ToUpper(pkg[:1]) + pkg[1:]
	if fn.Name.Name == titlePkg {
        return // worker.Worker-style exception: name == package name exactly
	}
	if strings.HasPrefix(fn.Name.Name, titlePkg) && wordBoundaryAfter(fn.Name.Name, len(titlePkg)) {
		astutil.Report(pass, fn.Name.Pos(), "naming-13", "%s.%s repeats the package name, use %s.%s instead", pkg, fn.Name.Name, pkg, fn.Name.Name[len(titlePkg):])
	}
})
```

**Exclusions:** unexported symbols (no consumer sees the stutter through the package qualifier);
methods (receiver methods stutter differently — a receiver's own type name, not the package's —
covered by `naming-14`); the exact-match exception (`Worker` in `package worker`, `Time` in
`package time`) per the source's own carve-out; a prefix hit not at a word boundary (e.g. a
package `task` with an exported `Tasking` type — `Task` is a prefix of `Tasking` but the next
rune `i` is lowercase, so no word boundary, no stutter — that's an unrelated word, not a repeat).

**Finding.Message:** `"%s.%s repeats the package name, use %s.%s instead"` (pkg, name, pkg, name-with-prefix-stripped)

**Finding.Severity:** Warning. Widens every call site and reads as generated rather than
designed — real API-ergonomics cost across every consumer, but not a defect; matches
`naming-08`'s tier for a similarly wide-blast-radius pure-convention issue.

---

### naming-14 — method name stutters its receiver type

**Source:** "Same principle on methods. `token.Validate()` over `token.ValidateToken()`,
`token.IsExpired()` over `token.IsTokenExpired()`."

**Checkable:** yes — exported method whose name contains its receiver's base type name as a
suffix at a word boundary (`ValidateToken` on receiver type `Token` → suffix `Token`, preceded
by a word boundary at `e`/`T`).

**Predicate** (`*ast.FuncDecl` with `Recv != nil`):

```go
insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv == nil || !fn.Name.IsExported() {
		return
	}
	recvType := strings.TrimPrefix(receiverTypeExprName(fn.Recv), "*")
	name := fn.Name.Name
	if name == recvType {
		return // e.g. a method literally named after the type is a different, rarer pattern
	}
	idx := strings.LastIndex(name, recvType)
	if idx > 0 && idx+len(recvType) == len(name) && wordBoundaryAfter(name, idx) {
		astutil.Report(pass, fn.Name.Pos(), "naming-14", "%s.%s stutters the receiver type name, rename to %s.%s", recvType, name, recvType, name[:idx])
	}
})
```

`receiverTypeExprName` is a one-line helper reading `fn.Recv.List[0].Type`'s `*ast.Ident.Name`
(unwrapping a leading `*ast.StarExpr` for pointer receivers) — trivial, omitted per the
predicate-body-only scope.

**Exclusions:** the suffix must land exactly at the end of the name (`idx+len(recvType) ==
len(name)`) and start at a word boundary — this excludes a method like `Tokenize` on a type
`Token` (contains "Token" as a prefix, not a stuttering suffix, and is a real distinct word, not
a repeat).

**Finding.Message:** `"%s.%s stutters the receiver type name, rename to %s.%s"` (recvType, name, recvType, name-with-suffix-stripped)

**Finding.Severity:** Warning. Same ergonomics cost as `naming-13`, scoped to one type instead
of the whole package.

---

### naming-15 — receiver named `this`, `self`, or `me`

**Source:** "Never `this`, `self`, or `me`, Go is not OO."

**Checkable:** yes — exact-match against a 3-element list, no ambiguity.

**Predicate** (`*ast.FuncDecl.Recv`):

```go
var bannedReceiverNames = map[string]bool{"this": true, "self": true, "me": true}

insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv == nil || len(fn.Recv.List[0].Names) == 0 {
		return
	}
	recv := fn.Recv.List[0].Names[0]
	if bannedReceiverNames[recv.Name] {
		astutil.Report(pass, recv.Pos(), "naming-15", "receiver named %q, Go is not OO — use a short type abbreviation instead", recv.Name)
	}
})
```

**Exclusions:** a receiver with no name (`func (Server) Start()`, blank/unnamed) is out of
scope — nothing to flag.

**Finding.Message:** `"receiver named %q, Go is not OO — use a short type abbreviation instead"` (name)

**Finding.Severity:** Warning. Doesn't break anything, but signals a mental-model mismatch the
reference calls out explicitly as a readability/idiom smell, one tier above pure cosmetic Info
because it recurs on every method of the type, compounding the friction.

---

### naming-16 — receiver longer than 3 characters

**Source:** "Short: 1-3 characters, an abbreviation of the type. `w` or `wrk` for `Worker`,
`hs` for `HighScore`, `p` for `Parser`."

**Checkable:** yes, as a length check — the only judgment-free part of this clause; whether the
abbreviation is a *good* one for the type is not checkable (that's taste), but the length ceiling
is a hard number the source itself gives.

**Predicate:**

```go
insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv == nil || len(fn.Recv.List[0].Names) == 0 {
		return
	}
	recv := fn.Recv.List[0].Names[0]
	if len(recv.Name) > 3 {
		astutil.Report(pass, recv.Pos(), "naming-16", "receiver %q is %d characters, keep receivers to 1-3 characters", recv.Name, len(recv.Name))
	}
})
```

**Exclusions:** none — the source states the ceiling as a flat number with no carve-out, and an
unnamed/blank receiver is already excluded structurally (no `Names` entry to inspect).

**Finding.Message:** `"receiver %q is %d characters, keep receivers to 1-3 characters"` (name, len)

**Finding.Severity:** Info. Pure style; longer receivers still read fine, just against
convention.

---

### naming-17 — inconsistent receiver name across one type's methods

**Source:** "The receiver name must be identical across every method of the type. `c` in one
method and `cus` in another is a code smell."

**Checkable:** yes — a whole-pass aggregation (not a single-node predicate): collect every
method's receiver name keyed by receiver type across the entire package, then flag every
occurrence once more than one distinct name exists for the same type.

**Predicate** (pass-level accumulation, run once after the `insp.Preorder` walk populates the
map — same two-phase shape as `observability-03`'s multi-`SetDefault`-call count):

```go
receiverNamesByType := map[string]map[string][]*ast.Ident{} // type -> name -> occurrences

insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv == nil || len(fn.Recv.List[0].Names) == 0 {
		return
	}
	recv := fn.Recv.List[0].Names[0]
	t := strings.TrimPrefix(receiverTypeExprName(fn.Recv), "*")
	if receiverNamesByType[t] == nil {
		receiverNamesByType[t] = map[string][]*ast.Ident{}
	}
	receiverNamesByType[t][recv.Name] = append(receiverNamesByType[t][recv.Name], recv)
})
for t, byName := range receiverNamesByType {
	if len(byName) < 2 {
		continue
	}
	for name, occs := range byName {
		for _, id := range occs {
			astutil.Report(pass, id.Pos(), "naming-17", "receiver %q on type %s is inconsistent with another method's receiver name for the same type", name, t)
		}
	}
}
```

**Exclusions:** types with exactly one distinct receiver name across all their methods
(the common, correct case) never enter the inner loop (`len(byName) < 2` short-circuits).
Pointer vs. value receiver on the same type sharing the same name (`w *Worker` and
`w Worker`) is correctly treated as consistent — `receiverTypeExprName` strips the leading
`*` before keying the map.

**Finding.Message:** `"receiver %q on type %s is inconsistent with another method's receiver name for the same type"` (name, type)

**Finding.Severity:** Warning. The source explicitly calls this a "code smell," and per
`naming.md`'s own critical-trap #1, an inconsistent receiver name across a type can mask a
value-vs-pointer receiver mismatch bug, not just read poorly — real correctness-adjacent risk.

---

### naming-18 — getter uses `Get` prefix

**Source:** "Getter: no `Get` prefix, `Address()` not `GetAddress()`."

**Checkable:** yes, as a name-shape check — `Get` followed immediately by an uppercase letter
or digit (word boundary), matching an established, low-false-positive lint already codified in
`staticcheck`'s `ST1003` and `revive`'s equivalent rule.

**Predicate:**

```go
var getPrefixRe = regexp.MustCompile(`^Get[A-Z0-9]`)

insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
	fn := n.(*ast.FuncDecl)
	if fn.Recv == nil || !getPrefixRe.MatchString(fn.Name.Name) {
		return
	}
	astutil.Report(pass, fn.Name.Pos(), "naming-18", "method %s uses the Get prefix, use %s instead", fn.Name.Name, fn.Name.Name[3:])
})
```

**Exclusions:** package-level functions (not methods) named `GetX` are excluded — "getter" as a
concept applies to a method retrieving a receiver's own field/state; a free function named
`GetEnv` or similar isn't the pattern this rule targets. No exclusion for interface-satisfaction
collisions is added because no stdlib interface in this project's dependency set (`go-sdk`,
`golang.org/x/tools`) requires an exact `GetXxx` method name — if one is discovered later, add
an explicit allowlist entry rather than loosening the regex.

**Finding.Message:** `"method %s uses the Get prefix, use %s instead"` (name, name-without-Get)

**Finding.Severity:** Warning. Pure API-ergonomics convention; matches the severity tier of the
other exported-symbol-shape rules (`naming-13`, `naming-14`).

---

### naming-19 — EXCLUDED: `Set` prefix / `Is`/`Has`/`Can` enforcement

**Source:** "Setter: `Set` prefix, `SetAddress()`. Keep `Is`/`Has`/`Can` prefixes for booleans:
`IsConnected()` not `Connected()`."

**Why excluded, not encoded as a heuristic:** unlike the `Get`-prefix ban (a negative pattern —
flag a specific banned prefix), these are positive prescriptions with no banned shape to detect.
"This single-param, void-returning method is a setter that should have had a `Set` prefix" is a
semantic-intent judgment a syntactic pass can't make from shape alone (plenty of legitimate
single-param void methods aren't setters — `Add`, `Close(error)`, `Publish(event)`). Flagging
every bool-returning, zero-param method that lacks an `Is`/`Has`/`Can` prefix produces
unacceptable false-positive volume against real stdlib and project precedent (`strings.Contains`,
`errors.Is` itself, `sort.IsSorted` is the exception not the rule — most bool-returning stdlib
methods use a plain verb or noun, not the prefix). Not encoded.

---

### naming-20 — single-method interface missing `-er`/`-r` suffix

**Source:** "Single-method interface: method name plus `-er` suffix, `io.Reader`, `io.Writer`,
`fmt.Stringer`, `http.Handler`."

**Checkable:** yes, for the mechanical suffix-formation rule the examples themselves follow:
interface name equals `MethodName + "er"`, or `MethodName + "r"` when `MethodName` already ends
in `"e"` (covers `Handle` → `Handler`, `Close` → `Closer`). Deliberately narrow — this catches
"named nothing like the method" (the actual failure mode), not every irregular English
suffix rule (`Stringer` from `String` fits the `+"er"` branch directly, no special case needed).

**Predicate** (`*ast.InterfaceType` with exactly one method):

```go
func matchesErSuffix(interfaceName, methodName string) bool {
	if interfaceName == methodName+"er" {
		return true
	}
	return strings.HasSuffix(methodName, "e") && interfaceName == methodName+"r"
}

insp.WithStack([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node, push bool, _ []ast.Node) bool {
	if !push {
		return true
	}
	ts := n.(*ast.TypeSpec)
	it, ok := ts.Type.(*ast.InterfaceType)
	if !ok || it.Methods == nil || len(it.Methods.List) != 1 {
		return true
	}
	method, ok := it.Methods.List[0].Type.(*ast.FuncType)
	_ = method
	if len(it.Methods.List[0].Names) != 1 {
		return true // embedded interface, not a named method — different shape, skip
	}
	methodName := it.Methods.List[0].Names[0].Name
	if !matchesErSuffix(ts.Name.Name, methodName) {
		astutil.Report(pass, ts.Name.Pos(), "naming-20", "single-method interface %s doesn't follow the %s-er naming convention for its %s method", ts.Name.Name, ts.Name.Name, methodName)
	}
	return true
})
```

**Exclusions:** an embedded-only interface (`type Foo interface { io.Reader }`, no `Names` on
that method entry) is skipped — it has no method of its own to derive a suffix from; multi-method
interfaces are excluded by the `len(...) != 1` guard, handled instead by `naming-22` (excluded)
and `naming-21`.

**Finding.Message:** `"single-method interface %s doesn't follow the %s-er naming convention for its %s method"` (interfaceName, interfaceName, methodName)

**Finding.Severity:** Info. Stdlib itself isn't 100% consistent on this (plenty of internal,
unexported single-method interfaces skip the suffix without harm) — low-stakes style guidance,
not a defect.

---

### naming-21 — interface name contains literal `Interface`

**Source:** "Never `Interface` in the name, `UserInterface` and `TaskInterface` are
anti-patterns."

**Checkable:** yes — exact substring match on the interface type's identifier, zero ambiguity.

**Predicate** (`*ast.TypeSpec` whose `Type` is `*ast.InterfaceType`, any method count):

```go
insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
	ts := n.(*ast.TypeSpec)
	if _, ok := ts.Type.(*ast.InterfaceType); !ok {
		return
	}
	if strings.Contains(ts.Name.Name, "Interface") {
		astutil.Report(pass, ts.Name.Pos(), "naming-21", "interface %s includes the word \"Interface\" in its name, drop it — the type declaration already says it's an interface", ts.Name.Name)
	}
})
```

**Exclusions:** none — "Interface" appearing anywhere in an interface type's name is
unconditionally the anti-pattern the source names, no legitimate counter-case given or found in
this project's own four services' interface usage.

**Finding.Message:** `"interface %s includes the word \"Interface\" in its name, drop it — the type declaration already says it's an interface"` (name)

**Finding.Severity:** Warning. Every implementer and caller reads the redundant word forever;
higher nuisance than `naming-20`'s suffix-formation nit because it's unambiguously wrong with no
stdlib exception, but still purely cosmetic — no behavior change, so below Error.

---

### naming-22 — EXCLUDED: multi-method interface "descriptive noun"

**Source:** "Multi-method interface: descriptive noun, `Authorizer`, `Authenticator`."

**Why excluded, not encoded as a heuristic:** "descriptive" is not a syntactic property — a
static pass has no model of whether a name conveys the right domain concept to a reader; the
only thing left to check mechanically (does the name end in a noun-like suffix) would degrade
into policing English morphology, a much weaker and noisier proxy than the actual ask. The
sibling `-er` suffix guidance shown in the same examples (`Authorizer`, `Authenticator`) is
already covered structurally by the single-method case (`naming-20`) when applicable; for a
genuine multi-method interface there is no mechanical substitute for "is this name descriptive."
Not encoded.

---

### naming-23 — RETIRED (cross-referenced): enum's first constant uses bare `iota`

**Source:** "Start real values at `iota + 1`, never bare `iota` for the first case... Reserve
zero for `Unspecified` or `Unknown`."

**Why retired here:** this exact check — a `const` block whose declared type is a named
(non-builtin) integer type and whose first `*ast.ValueSpec` initializes with the bare identifier
`iota` — is already implemented as `typedesign-08` in `phase-4a-type-design.md`, predicate and all
(type design owns enum-shape rules generally, including the sibling "start at `iota + 1`"
convention). Implementing it a second time under a different `Finding.Rule` would produce two
findings for the same defect from two different audit tools, which is worse than one tool
reporting it once. Not implemented here; callers wanting this check run `go_audit_typedesign`.
This correctly leaves naming with no Error-severity rule — naming's remaining defects are all
convention/readability, not the correctness-adjacent zero-value-enum trap `typedesign-08` guards.

---

### naming-24 — enum constant missing type-name prefix

**Source:** "Prefix each constant with the type name, proto-style: `StatusPending`,
`StatusConfirmed`, not bare `Pending`, `Confirmed`."

**Checkable:** yes, for a `const` block already identified as an enum-shaped group (named
integer type, resolved via `pass.TypesInfo.TypeOf` on the first spec's declared `Type`, same
type-resolution shape `typedesign-08` uses for its own bare-`iota` check) — each constant's name
should start with the type's own name.

**Predicate** (`*ast.GenDecl`, `Tok == token.CONST`, resolving the const block's declared type
inline):

```go
insp.Preorder([]ast.Node{(*ast.GenDecl)(nil)}, func(n ast.Node) {
	gd := n.(*ast.GenDecl)
	if gd.Tok != token.CONST {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, name := range vs.Names {
			obj := pass.TypesInfo.Defs[name]
			if obj == nil {
				continue
			}
			t := obj.Type()
			if t == nil || types.Universe.Lookup(t.String()) != nil {
				continue
			}
			typeName := t.String()
			if idx := strings.LastIndexByte(typeName, '.'); idx >= 0 {
				typeName = typeName[idx+1:] // strip package qualifier from types.Type.String()
			}
			if !strings.HasPrefix(name.Name, typeName) {
				astutil.Report(pass, name.Pos(), "naming-24", "enum constant %s doesn't carry its type name %s as a prefix", name.Name, typeName)
			}
		}
	}
})
```

**Exclusions:** resolving each constant's type via `pass.TypesInfo.Defs[name].Type()` (the
constant identifier's own resolved type, from the type-checker's object table) rather than
`vs.Type` means every constant in a `const` block is checked, not just the one `*ast.ValueSpec`
that carries an explicit `Type` node (typically the first) — later specs in the same block
inherit the type implicitly with no `vs.Type` to read, but `Defs[name]` still resolves it
correctly via the type-checker.

**Finding.Message:** `"enum constant %s doesn't carry its type name %s as a prefix"` (name, typeName)

**Finding.Severity:** Info. Readability-only — an unprefixed enum constant still functions
correctly, it just reads ambiguously at the call site (`Pending` vs. `StatusPending`).

---

### naming-25 — EXCLUDED: constants named by role, not value

**Source:** "Name by role, not by value. `MaxPacketSize` not `MAX_PACKET_SIZE`,
`DefaultTimeout` not `DEFAULT_TIMEOUT`. Go does not use SCREAMING_SNAKE for constants."

**Why excluded, not encoded as a heuristic:** "named by role vs. by value" is a semantic
judgment about what the constant represents, not a syntactic property — nothing in the AST
distinguishes a well-named role-based constant from a poorly-named one at the same casing. The
one syntactically-checkable clause here — "Go does not use SCREAMING_SNAKE for constants" — is
not new content: it's the general snake_case/SCREAMING_SNAKE ban already implemented as
`naming-01`, restated in this source specifically for the constants case. Not re-implemented as
a separate rule to avoid two `Finding.Rule`s firing on the same defect.

---

### naming-26 — sentinel error var missing `Err`/`err` prefix

**Source:** "Exported sentinel errors: `ErrFoo`. Unexported sentinels: `errFoo`."

**Checkable:** yes — a package-level `var` whose static type is exactly the `error` interface
(via a small local `isErrorType` helper — see Shared helpers) and whose initializer is an
`errors.New`/`fmt.Errorf` call (via a small local `isFmtErrorfOrNew` helper, same section) is
unambiguously a sentinel — the export-status-matched prefix check follows directly from its name
and `IsExported()`. `phase-4a-errors.md` documents its own copy of this same predicate shape for a
different rule; the two are not shared code (naming and errors are separate packages per
CONTRACTS' subpackage-per-domain model), just parallel logic — flagged as a candidate for
promotion to `astutil` if a third domain needs it, not decided here.

**Predicate** (`*ast.ValueSpec` at package scope):

```go
insp.Preorder([]ast.Node{(*ast.ValueSpec)(nil)}, func(n ast.Node) {
	vs := n.(*ast.ValueSpec)
	for i, name := range vs.Names {
		if pass.TypesInfo.Defs[name] == nil || pass.TypesInfo.Defs[name].Parent() != pass.Pkg.Scope() {
			continue // only package-level vars are sentinel candidates
		}
		if !isErrorType(pass, name) || i >= len(vs.Values) {
			continue
		}
		call, ok := vs.Values[i].(*ast.CallExpr)
		if !ok {
			continue
		}
		if _, _, ok := isFmtErrorfOrNew(pass, call); !ok {
			continue
		}
		wantPrefix := "err"
		if name.IsExported() {
			wantPrefix = "Err"
		}
		if !strings.HasPrefix(name.Name, wantPrefix) {
			astutil.Report(pass, name.Pos(), "naming-26", "sentinel error %s doesn't carry the %s prefix", name.Name, wantPrefix)
		}
	}
})
```

**Exclusions:** function-local `error` vars (not package-scope) are excluded — a sentinel is by
definition a shared, reusable value, and a local `err := errors.New(...)` inside a function body
is either dead code or an unusual pattern outside this rule's intent, not a sentinel; vars
initialized from something other than `errors.New`/`fmt.Errorf` (e.g. wrapping another package's
sentinel, `var ErrTimeout = context.DeadlineExceeded`) are excluded by the `isFmtErrorfOrNew`
guard — those are aliases, not newly-minted sentinels, and already correctly named by
construction if they alias a well-named upstream error.

**Finding.Message:** `"sentinel error %s doesn't carry the %s prefix"` (name, wantPrefix)

**Finding.Severity:** Warning. `errors.Is` callers rely on grep-ability of the `Err`/`err`
convention to find every sentinel in a package at a glance; missing it doesn't break
`errors.Is` matching itself (that's identity-based, not name-based) but degrades discoverability
during an incident when someone greps for `Err` to enumerate known failure modes.

---

### naming-27 — custom error type missing `Error` suffix

**Source:** "Custom error types: suffix `Error` (`PathError`, `ValidationError`)."

**Checkable:** yes — a named type whose method set implements the `error` interface
(`types.Implements`, the same mechanism `errors-02` in `phase-4a-errors.md` uses to detect a concrete
error type) is unambiguously "a custom error type"; whether its name ends in `Error` is a plain
suffix check.

**Predicate** (`*ast.TypeSpec`, resolved via `go/types`):

```go
insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
	ts := n.(*ast.TypeSpec)
	obj := pass.TypesInfo.Defs[ts.Name]
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return
	}
	errIface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	implements := types.Implements(tn.Type(), errIface) || types.Implements(types.NewPointer(tn.Type()), errIface)
	if !implements || ts.Name.Name == "error" {
		return
	}
	if !strings.HasSuffix(ts.Name.Name, "Error") {
		astutil.Report(pass, ts.Name.Pos(), "naming-27", "type %s implements error but doesn't carry the Error suffix", ts.Name.Name)
	}
})
```

**Exclusions:** a type literally named `error` (shadowing the builtin — already caught
separately by `naming-04`) is skipped to avoid a duplicate finding on the same declaration; an
interface type that merely embeds `error` (satisfies `Implements` but `ts.Type` underlying is
`*types.Interface`, not a concrete struct) is a different pattern — add
`_, isIface := tn.Type().Underlying().(*types.Interface); !isIface` as an additional guard,
mirroring `errors-02`'s identical exclusion in `phase-4a-errors.md`.

**Finding.Message:** `"type %s implements error but doesn't carry the Error suffix"` (name)

**Finding.Severity:** Warning. Discoverability convention (`errors.As` target types are easiest
to find by the `Error` suffix, same grep-ability argument as `naming-26`) — no functional
impact, `errors.As` matches by type identity regardless of name.

---

### naming-28 — EXCLUDED (cross-referenced): error string casing/punctuation/acronyms

**Source:** "Error strings: lowercase, no ending punctuation, no capitalized acronyms. This
pairs with the error-handling guide, which covers wrapping and translation at service
boundaries."

**Why excluded here:** this exact check — first-rune case, trailing punctuation, embedded
`[A-Z]{2,}` acronym run on an `errors.New`/`fmt.Errorf` literal — is already implemented as
`errors-07` in `phase-4a-errors.md`, predicate and all. The source file explicitly flags this as
shared territory with the error-handling guide; implementing it a second time under a different
`Finding.Rule` would produce two findings for the same defect from two different audit tools,
which is worse than one tool reporting it once. Not re-implemented; callers wanting this check
run `go_audit_errors`.

---

## 3. Fixture file spec

Per `contracts.md`'s "Testdata fixture layout — one isolated package per rule": one Go
package per rule, `internal/tools/testdata/fixtures/audit-naming/rule<NN>/`, zero-padded to 2
digits. Each rule's directory holds exactly one violating file and, where a false-positive risk
exists, one compliant counterpart — never a mixed file. Package name is `fixture` for every rule
directory (short, not `util`/`helper`/`common`, no stutter against the domain) — the one property
CONTRACTS leaves to each domain file to pick, since it's outside the byte-for-byte conformance
block.

**Exceptions (both required by the rule's own detection target, not invented):**

- `naming-08`/`naming-09` check the package **clause** itself — a single directory cannot hold
  two conflicting `package` names, so each gets nested `rule08/violation/` + `rule08/compliant/`
  (and equivalently `rule09/`) sub-packages instead of the flat `violation.go`/`compliant.go`
  pair.
- `naming-11` checks the **file name**'s own casing — a file literally named `violation.go` is
  already casing-compliant by construction, so it cannot host naming-11's violation. `rule11/`
  therefore uses `BadFile.go` (violation) and `ok_file.go` (compliant) in place of the standard
  names. Both deviations are flagged again in §5, since the standard `TestAudit<Domain>_Rule<NN>`
  template's fixed `.../rule<NN>/violation.go` path literal doesn't hold for these three rules.

### Rule → file map

| Rule ID | File(s) | Violation case | Near-miss case |
|---|---|---|---|
| `naming-01` | `rule01/violation.go`, `rule01/compliant_test.go` | `max_retries` (snake_case var) | `TestParse_InvalidBitmap` in a `_test.go` file (test-subcase-name exclusion) |
| `naming-02` | `rule02/violation.go`, `rule02/compliant.go` | `ApiKey` (Title-cased acronym) | `APIKey` (all-upper) |
| `naming-03` | `rule03/violation.go`, `rule03/compliant.go` | `countInt = 5` (int-typed, `Int` suffix) | `userIDStr := strconv.Itoa(userID)` (sanctioned conversion suffix) |
| `naming-04` | `rule04/violation.go`, `rule04/compliant.go` | param `len` shadows builtin | param renamed `length` |
| `naming-05` | `rule05/violation.go`, `rule05/compliant.go` | local `log := ...` | package-level `var log = ...` (excluded: package scope) |
| `naming-08` | `rule08/violation/violation.go`, `rule08/compliant/compliant.go` | `package bad_pkg` (underscore) | `package parser2` (digit, no underscore/upper) |
| `naming-09` | `rule09/violation/violation.go`, `rule09/compliant/compliant.go` | `package util` (catch-all) | `package utils` (not an exact list match) |
| `naming-11` | `rule11/BadFile.go`, `rule11/ok_file.go` | uppercase filename | lowercase+underscore filename |
| `naming-13` | `rule13/violation.go`, `rule13/compliant.go` | `FixtureValidate()` | `type Fixture struct{}` (exact-match exception) |
| `naming-14` | `rule14/violation.go`, `rule14/compliant.go` | `(*Token) ValidateToken()` | `(*Token) Tokenize()` (prefix, not suffix) |
| `naming-15` | `rule15/violation.go`, `rule15/compliant.go` | receiver `this` | receiver `g` |
| `naming-16` | `rule16/violation.go`, `rule16/compliant.go` | receiver `parser` (6 chars) | receiver `p` (1 char) |
| `naming-17` | `rule17/violation.go`, `rule17/compliant.go` | `q` vs `queue` on `Queue` | `r`/`r` (ptr+value) on `Registry` |
| `naming-18` | `rule18/violation.go`, `rule18/compliant.go` | method `GetSize()` | free func `DefaultCacheKey()` (not a method — excluded) |
| `naming-20` | `rule20/violation.go`, `rule20/compliant.go` | `DataLoader{ Load() }` (wants `Loader`) | `Closer{ Close() }` (`e`-suffix rule) |
| `naming-21` | `rule21/violation.go`, `rule21/compliant.go` | `TaskInterface` | `TaskProcessor` |
| `naming-24` | `rule24/violation.go`, `rule24/compliant.go` | `Red Color = iota + 1` | `ModeActive Mode = iota + 1` |
| `naming-26` | `rule26/violation.go`, `rule26/compliant.go` | `var NotFound = errors.New(...)` | `var ErrNotFound = errors.New(...)` |
| `naming-27` | `rule27/violation.go`, `rule27/compliant.go` | `PathFailure` implements `error` | `PathError` implements `error` |

38 fixture files total: 17 rules × 2 files (`violation.go` + compliant counterpart) = 34, plus
`naming-08`/`naming-09` × 2 files each (nested `violation/`+`compliant/` sub-packages) = 4.
`naming-23` contributes zero — retired, no fixture (see §1/§2).

### File contents

`rule01/violation.go`
```go
package fixture

// VIOLATION: naming-01
var max_retries = 3
```

`rule01/compliant_test.go`
```go
package fixture

import "testing"

// COMPLIANT: naming-01 — test-subcase-name exclusion
func TestParse_InvalidBitmap(t *testing.T) {}
```

`rule02/violation.go`
```go
package fixture

// VIOLATION: naming-02
type ApiKey struct{}
```

`rule02/compliant.go`
```go
package fixture

// COMPLIANT: naming-02
type APIKey struct{}
```

`rule03/violation.go`
```go
package fixture

// VIOLATION: naming-03
var countInt = 5
```

`rule03/compliant.go`
```go
package fixture

import "strconv"

// COMPLIANT: naming-03 — sanctioned conversion-suffix exception
func convert(userID int) string {
	userIDStr := strconv.Itoa(userID)
	return userIDStr
}
```

`rule04/violation.go`
```go
package fixture

// VIOLATION: naming-04
func sum(len int) int { return len }
```

`rule04/compliant.go`
```go
package fixture

// COMPLIANT: naming-04
func sumFixed(length int) int { return length }
```

`rule05/violation.go`
```go
package fixture

// VIOLATION: naming-05
func shadowLog() {
	log := "local var named after stdlib package"
	_ = log
}
```

`rule05/compliant.go`
```go
package fixture

// COMPLIANT: naming-05 — package-level declaration, not a local shadow
var log = "package-level, excluded by construction"
```

`rule08/violation/violation.go`
```go
package bad_pkg // VIOLATION: naming-08 — package name contains an underscore

var Placeholder = struct{}{}
```

`rule08/compliant/compliant.go`
```go
package parser2 // COMPLIANT: naming-08 — lowercase with a digit, no underscore/upper/non-ASCII

var Placeholder = struct{}{}
```

`rule09/violation/violation.go`
```go
package util // VIOLATION: naming-09 — catch-all dumping-ground package name

var Placeholder = struct{}{}
```

`rule09/compliant/compliant.go`
```go
package utils // COMPLIANT: naming-09 — "utils" is not an exact match for banned "util"

var Placeholder = struct{}{}
```

`rule11/BadFile.go`
```go
package fixture

// VIOLATION: naming-11 — file name "BadFile.go" contains uppercase letters
var placeholder = struct{}{}
```

`rule11/ok_file.go`
```go
package fixture

// COMPLIANT: naming-11 — lowercase with underscores, no hyphens/uppercase
var placeholderOK = struct{}{}
```

`rule13/violation.go`
```go
package fixture

// VIOLATION: naming-13 — repeats package name "fixture"
func FixtureValidate() bool { return true }
```

`rule13/compliant.go`
```go
package fixture

// COMPLIANT: naming-13 — name equals package name exactly (exact-match exception)
type Fixture struct{}
```

`rule14/violation.go`
```go
package fixture

type Token struct{}

// VIOLATION: naming-14
func (t *Token) ValidateToken() bool { return true }
```

`rule14/compliant.go`
```go
package fixture

// COMPLIANT: naming-14 — "Token" is a prefix of "Tokenize", not a stuttering suffix
func (t *Token) Tokenize() string { return "" }
```

`rule15/violation.go`
```go
package fixture

type Widget struct{}

// VIOLATION: naming-15
func (this *Widget) Render() {}
```

`rule15/compliant.go`
```go
package fixture

type Gadget struct{}

// COMPLIANT: naming-15
func (g *Gadget) Render() {}
```

`rule16/violation.go`
```go
package fixture

type LongRecv struct{}

// VIOLATION: naming-16 — receiver "parser" is 6 characters
func (parser *LongRecv) Parse() {}
```

`rule16/compliant.go`
```go
package fixture

type ShortRecv struct{}

// COMPLIANT: naming-16
func (p *ShortRecv) Parse() {}
```

`rule17/violation.go`
```go
package fixture

type Queue struct{ Size int }

func (q *Queue) Add(item string) { q.Size++ }

// VIOLATION: naming-17 — receiver "queue" inconsistent with "q" used on Add above
func (queue *Queue) Remove(item string) { queue.Size-- }
```

`rule17/compliant.go`
```go
package fixture

type Registry struct{ Count int }

func (r *Registry) Register(name string) { r.Count++ }

// COMPLIANT: naming-17 — pointer vs value receiver, same name "r"
func (r Registry) Lookup(name string) int { return r.Count }
```

`rule18/violation.go`
```go
package fixture

type Cache struct{ size int }

// VIOLATION: naming-18
func (c *Cache) GetSize() int { return c.size }
```

`rule18/compliant.go`
```go
package fixture

// COMPLIANT: naming-18 — package-level function, not a method; "getter" doesn't apply
func DefaultCacheKey() string { return "" }
```

`rule20/violation.go`
```go
package fixture

// VIOLATION: naming-20 — interface name doesn't derive from method name (want "Loader")
type DataLoader interface {
	Load() error
}
```

`rule20/compliant.go`
```go
package fixture

// COMPLIANT: naming-20 — Close ends in "e", so Close+"r" = Closer
type Closer interface {
	Close() error
}
```

`rule21/violation.go`
```go
package fixture

// VIOLATION: naming-21
type TaskInterface interface {
	Submit() error
	Cancel() error
}
```

`rule21/compliant.go`
```go
package fixture

// COMPLIANT: naming-21
type TaskProcessor interface {
	Submit() error
	Cancel() error
}
```

`rule24/violation.go`
```go
package fixture

type Color int

const (
	// VIOLATION: naming-24 — missing "Color" prefix
	Red Color = iota + 1
	Green
)
```

`rule24/compliant.go`
```go
package fixture

type Mode int

const (
	// COMPLIANT: naming-24
	ModeActive Mode = iota + 1
	ModeInactive
)
```

`rule26/violation.go`
```go
package fixture

import "errors"

// VIOLATION: naming-26 — exported sentinel missing Err prefix
var NotFound = errors.New("not found")
```

`rule26/compliant.go`
```go
package fixture

import "errors"

// COMPLIANT: naming-26
var ErrNotFound = errors.New("not found")
```

`rule27/violation.go`
```go
package fixture
// VIOLATION: naming-27 — implements error, no Error suffix
type PathFailure struct{ Path string }

func (e *PathFailure) Error() string { return "path failure: " + e.Path }
```

`rule27/compliant.go`
```go
package fixture

// COMPLIANT: naming-27
type PathError struct{ Path string }

func (e *PathError) Error() string { return "path error: " + e.Path }
```

## 4. Tool file spec — `internal/tools/go_audit_naming.go`

Reproduced verbatim from `contracts.md`'s "Conformance block — copy verbatim into every domain
file's §4," `<domain>`→`naming`, `<Domain>`→`Naming`.

```go
package tools

import (
	"context"
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/naming"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditNamingInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditNamingOutput struct {
	Result finding.AuditResult `json:"result"`
}

func AuditNamingHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditNamingInput) (*mcp.CallToolResult, AuditNamingOutput, error) {
	if err := normalizeAuditNamingInput(&in); err != nil {
		return nil, AuditNamingOutput{}, fmt.Errorf("validating input: %w", err)
	}
	ws, err := resolveInWorkspace(in.Package)
	if err != nil {
		return nil, AuditNamingOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{naming.Analyzer})
	if err != nil {
		return nil, AuditNamingOutput{}, fmt.Errorf("running naming audit: %w", err)
	}
	return nil, AuditNamingOutput{Result: result}, nil
}

func RegisterAuditNaming(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_audit_naming",
		Description: "Audits a Go package for naming-convention violations (casing, stutter, receivers, enums, sentinels) and returns structured findings.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, AuditNamingHandler)
}

func normalizeAuditNamingInput(in *AuditNamingInput) error {
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

`resolveInWorkspace` is defined once, project-wide (never redeclared here). `naming.Analyzer` is
the single exported `*analysis.Analyzer` from `internal/analysis/naming/` (§2's rules, `package
naming`) — `audit.Run` is the sole caller of `checker.Analyze`; this file never calls
`packages.Load`, `checker.Analyze`, or `pass.Run` directly, and never redeclares `runNaming`,
`runNamingAnalyzer`, or `mustRunNaming`. `ToolAnnotations` above are the read-only-audit defaults;
`go_audit_naming` performs no writes, so no override is needed.

---

## 5. Verification spec

Reproduced per `contracts.md`'s "§5 verification — per-domain, per-rule" template: one
`TestAuditNaming_Rule<NN>` / `TestAuditNaming_Rule<NN>_CompliantIsSilent` pair per rule (no mega
`TestAuditNaming` with `t.Run` subtests, no `findingsForRule`/`mustRunNaming` helpers — both
banned), plus one `TestAuditNaming_TotalRuleCount`. `naming-17` and `naming-24` deviate from the
single-`Finding` template with their own multi-`Finding` bodies (2 Findings each — see notes);
`naming-08`/`naming-09` deviate on fixture path (nested `rule<NN>/violation/` and
`rule<NN>/compliant/` sub-packages, per §3's exception); `naming-11` deviates only on filename
(`BadFile.go`/`ok_file.go`, not `violation.go`/`compliant.go`, per §3's exception) but stays in one
package like the rest.

**Note on `_CompliantIsSilent`:** CONTRACTS' literal template runs the fixture once and asserts no
returned `Finding` anywhere carries the rule's own ID — for the 17 rules whose violation and
compliant files share one package/directory, the genuine violation-file finding (asserted to exist
by the sibling `TestAuditNaming_Rule<NN>` test) *does* carry that exact rule ID, so a literal,
unfiltered "no finding has this Rule" assertion over the whole fixture load cannot pass alongside
it. Reproduced verbatim anyway per the task's byte-for-byte instruction; flagged in this pass's
report as a CONTRACTS-level tension, not fixed here (CONTRACTS is canonical and out of scope for
edits). `naming-08`/`naming-09` incidentally don't hit this, since their violation/compliant fixtures
load from separate nested sub-packages.

```go
func TestAuditNaming_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule01")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-01", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule01/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule01")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule01/compliant_test.go" {
			assert.NotEqual(t, "naming-01", f.Rule, "compliant_test.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule02(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule02")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-02", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule02/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule02_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule02")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule02/compliant.go" {
			assert.NotEqual(t, "naming-02", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule03(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule03")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-03", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule03/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule03_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule03")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule03/compliant.go" {
			assert.NotEqual(t, "naming-03", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule04(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule04")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-04", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule04/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule04_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule04")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule04/compliant.go" {
			assert.NotEqual(t, "naming-04", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule05(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule05")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-05", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule05/violation.go", f.Location.File)
	assert.Equal(t, 5, f.Location.Line)
}

func TestAuditNaming_Rule05_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule05")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule05/compliant.go" {
			assert.NotEqual(t, "naming-05", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule08(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule08/violation")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-08", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule08/violation/violation.go", f.Location.File)
	assert.Equal(t, 1, f.Location.Line)
}

func TestAuditNaming_Rule08_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule08/compliant")
	for _, f := range findings {
		assert.NotEqual(t, "naming-08", f.Rule, "compliant.go must not trigger its own rule")
	}
}

func TestAuditNaming_Rule09(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule09/violation")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-09", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule09/violation/violation.go", f.Location.File)
	assert.Equal(t, 1, f.Location.Line)
}

func TestAuditNaming_Rule09_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule09/compliant")
	for _, f := range findings {
		assert.NotEqual(t, "naming-09", f.Rule, "compliant.go must not trigger its own rule")
	}
}
func TestAuditNaming_Rule11(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule11")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-11", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule11/BadFile.go", f.Location.File)
	assert.Equal(t, 1, f.Location.Line)
}

func TestAuditNaming_Rule11_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule11")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule11/ok_file.go" {
			assert.NotEqual(t, "naming-11", f.Rule, "ok_file.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule13(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule13")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-13", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule13/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule13_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule13")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule13/compliant.go" {
			assert.NotEqual(t, "naming-13", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule14(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule14")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-14", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule14/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditNaming_Rule14_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule14")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule14/compliant.go" {
			assert.NotEqual(t, "naming-14", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule15(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule15")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-15", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule15/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditNaming_Rule15_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule15")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule15/compliant.go" {
			assert.NotEqual(t, "naming-15", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule16(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule16")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-16", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule16/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditNaming_Rule16_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule16")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule16/compliant.go" {
			assert.NotEqual(t, "naming-16", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

// naming-17 deviates from the single-Finding template: the predicate (§2) reports every
// occurrence once a type has ≥2 distinct receiver names, so Queue's Add ("q") and
// Remove ("queue") both fire.
func TestAuditNaming_Rule17(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule17")
	var got []finding.Finding
	for _, f := range findings {
		if f.Rule == "naming-17" {
			got = append(got, f)
		}
	}
	require.Len(t, got, 2)
	assert.Equal(t, finding.SeverityWarning, got[0].Severity)
	assert.Equal(t, "fixtures/audit-naming/rule17/violation.go", got[0].Location.File)
	assert.Equal(t, 5, got[0].Location.Line)
	assert.Equal(t, "fixtures/audit-naming/rule17/violation.go", got[1].Location.File)
	assert.Equal(t, 8, got[1].Location.Line)
}

func TestAuditNaming_Rule17_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule17")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule17/compliant.go" {
			assert.NotEqual(t, "naming-17", f.Rule,
				"compliant.go's ptr+value receivers share one name and must not trigger naming-17")
		}
	}
}

func TestAuditNaming_Rule18(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule18")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-18", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule18/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditNaming_Rule18_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule18")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule18/compliant.go" {
			assert.NotEqual(t, "naming-18", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule20(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule20")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-20", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule20/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule20_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule20")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule20/compliant.go" {
			assert.NotEqual(t, "naming-20", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule21(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule21")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-21", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule21/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule21_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule21")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule21/compliant.go" {
			assert.NotEqual(t, "naming-21", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

// naming-24 deviates from the single-Finding template: §2's predicate resolves each constant's
// type via pass.TypesInfo.Defs[name].Type(), not vs.Type, so it fires on every constant in the
// block, including Green (which inherits Color implicitly, no explicit Type node of its own).
func TestAuditNaming_Rule24(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule24")
	var got []finding.Finding
	for _, f := range findings {
		if f.Rule == "naming-24" {
			got = append(got, f)
		}
	}
	require.Len(t, got, 2)
	assert.Equal(t, finding.SeverityInfo, got[0].Severity)
	assert.Equal(t, "fixtures/audit-naming/rule24/violation.go", got[0].Location.File)
	assert.Equal(t, 7, got[0].Location.Line) // Red
	assert.Equal(t, "fixtures/audit-naming/rule24/violation.go", got[1].Location.File)
	assert.Equal(t, 8, got[1].Location.Line) // Green
}

func TestAuditNaming_Rule24_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule24")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule24/compliant.go" {
			assert.NotEqual(t, "naming-24", f.Rule,
				"compliant.go's ModeActive/ModeInactive both carry the Mode prefix")
		}
	}
}

func TestAuditNaming_Rule26(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule26")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-26", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule26/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditNaming_Rule26_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule26")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule26/compliant.go" {
			assert.NotEqual(t, "naming-26", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_Rule27(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule27")
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "naming-27", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-naming/rule27/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditNaming_Rule27_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, naming.Analyzer, "audit-naming/rule27")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-naming/rule27/compliant.go" {
			assert.NotEqual(t, "naming-27", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditNaming_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("naming"), 19) // catches a rule silently dropped or added
}
```
