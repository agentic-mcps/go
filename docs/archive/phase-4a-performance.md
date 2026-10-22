# phase-4a-performance.md — `go_audit_performance`

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

Reads `contracts.md` first (canonical `Finding`/`AuditResult`/`Location`,
conformance block, `astutil`/`audit.Run` shapes, fixture layout) and
`phase-4a-index.md` (required 5-part structure, non-negotiable full-AST-pattern
rigor). This file is self-contained per the dispatch model — no other domain
file's content is assumed.

Fixture path uses the canonical form
`internal/tools/testdata/fixtures/audit-performance/rule<NN>/`. Sections 3
and 5 use that per-rule path exclusively.

The original performance corpus is reproduced in this specification. It has
14 numbered rules; source rules #3 and #11 restate the same
recommendation (preallocate to avoid `append` growth-copy churn) and are
merged into one rule ID below (`performance-03`). Source rule #8
(`sync.Pool.Put` reset) is excised entirely as a duplicate of the more
general `concurrency-14` — see §1 and §2. Result: 12 rule IDs, 9 checkable,
3 excluded.

---

## 1. Rules table

| Rule ID | Source rule(s) | One-line description | Status | Severity |
|---|---|---|---|---|
| `performance-01` | #1 | Measure before optimizing (bench + pprof before changing code) | **Excluded** | n/a |
| `performance-02` | #2 | Struct fields ordered largest-alignment → smallest | Checkable | Warning |
| `performance-03` | #3, #11 | Preallocate slice/map capacity when the final size is loop-bounded and known | Checkable | Warning |
| `performance-04` | #4 | `strings.Builder` for string concatenation in a loop, not `+=` | Checkable | Warning |
| `performance-05` | #5 | `strconv` over bare `fmt.Sprintf("%d", n)` for int-to-string | Checkable | Info |
| `performance-06` | #6 | Fixed string converted to `[]byte` once, not per loop iteration | Checkable | Warning |
| `performance-07` | #7 | Write to `io.Writer` directly (`fmt.Fprintf`), not `w.Write([]byte(fmt.Sprintf(...)))` | Checkable | Info |
| `performance-09` | #9 | Avoid `reflect` package calls inside a loop body (hot-path proxy) | Checkable (partial — see exclusion) | Warning |
| `performance-10` | #12 | Copy a small fixed window out of a caller-owned slice before returning; don't reslice-and-return | Checkable | Error |
| `performance-11` | #10 | Maps don't shrink after deletes; rebuild under high-delete churn | **Excluded** | n/a |
| `performance-12` | #13 | Benchmark hygiene: `b.ResetTimer()` after setup, before the `b.N` loop | Checkable | Info |
| `performance-13` | #14 | Least mechanism: don't add a dependency to avoid a short loop | **Excluded** | n/a |

**`performance-08` excised** (source rule #8: `sync.Pool.Put` of a
`*bytes.Buffer` must be preceded by `.Reset()`). Duplicate of the more
general `concurrency-14`, which already covers an unreset `Put` on any
pooled type and subsumes the `*bytes.Buffer`-specific case that lived here.
Kept once, canonical in `phase-4a-concurrency.md`. This rule's predicate,
exclusions, severity, fixture, and §5 test are removed below in their
entirety; no rule ID is reused. Per this pass's scope, no reciprocal note
was added to `phase-4a-concurrency.md` — that file belongs to a different
subagent.

Cross-reference (one line, per task instruction): `performance-02` overlaps
with Phase 5's standalone `go_field_alignment` tool, which wraps
`x/tools/go/analysis/passes/fieldalignment` and can `-fix` in place;
`performance-02` here is the read-only detection signal inside the
general-purpose sweep, kept because a caller running `go_audit_performance`
alone (without invoking the dedicated Phase 5 tool) should not miss padding
waste — same underlying signal, no `-fix` capability, not a duplicate tool.

---

## 2. Per-rule AST patterns

### Shared predicate helpers (defined once, referenced by name below)

```go
// insideLoop reports whether stack (from Inspector.WithStack) contains a
// *ast.ForStmt or *ast.RangeStmt ancestor of the current node.
func insideLoop(stack []ast.Node) bool {
    for _, a := range stack {
        switch a.(type) {
        case *ast.ForStmt, *ast.RangeStmt:
            return true
        }
    }
    return false
}

// isByteSliceConversion reports whether call is a type conversion to []byte,
// e.g. []byte("x") or []byte(s).
func isByteSliceConversion(info *types.Info, call *ast.CallExpr) bool {
    if len(call.Args) != 1 {
        return false
    }
    t := info.TypeOf(call.Fun)
    slice, ok := t.(*types.Slice)
    if !ok {
        return false
    }
    b, ok := slice.Elem().(*types.Basic)
    return ok && b.Kind() == types.Byte
}

// isConstantString reports whether expr's value is a compile-time constant
// string (BasicLit or a resolved *types.Const), not a runtime-varying value.
func isConstantString(info *types.Info, expr ast.Expr) bool {
    tv, ok := info.Types[expr]
    return ok && tv.Value != nil && tv.Value.Kind() == constant.String
}

// callsMethod reports whether stmt is a bare expression-statement call to
// methodName on the identifier named recv, e.g. `b.ResetTimer()` matches
// callsMethod(stmt, "b", "ResetTimer"). Used by performance-12 below; this
// file previously referenced callsMethod without declaring it — declared
// here to close that gap.
func callsMethod(stmt ast.Stmt, recv, methodName string) bool {
    exprStmt, ok := stmt.(*ast.ExprStmt)
    if !ok {
        return false
    }
    call, ok := exprStmt.X.(*ast.CallExpr)
    if !ok {
        return false
    }
    sel, ok := call.Fun.(*ast.SelectorExpr)
    if !ok || sel.Sel.Name != methodName {
        return false
    }
    id, ok := sel.X.(*ast.Ident)
    return ok && id.Name == recv
}
```

`isPkgIdent` is removed: it was this file's copy of the "is this a call to
pkg.Func" pattern that the plan's drift table flags as duplicated (under
different names) across all seven domain files. Its two call sites
(`performance-05`, `performance-07`) become `astutil.IsPkgFunc` below.
`performance-09`'s use of `isPkgIdent` was structurally different — it
matched *any* function in the `reflect` package, not one named function —
so it isn't a like-for-like replacement; see the inline, rule-scoped
`isAnyReflectCall` helper in that section instead of a second shared
generic-package-match helper.

### performance-02 — struct field alignment

> Source: "Order struct fields largest-alignment to smallest ... The runtime
> pads fields to their alignment, so an `int64` after a `bool` wastes 7 bytes."

**Checkable**: `go/types.Sizes` gives deterministic, compiler-accurate
`Sizeof`/`Alignof` per field — this is exact arithmetic, not a heuristic.

**Predicate** — `*ast.StructType` with ≥2 fields; compute actual size, then
compute the size of a struct with the same fields sorted by descending
`Alignof`; flag if the sorted size is strictly smaller (real padding waste,
not just a theoretical reorder with no effect). Returns a bool, like every
other predicate in this file — reporting is the caller's job (§4's
`run(pass *analysis.Pass)` dispatcher), never inline in the predicate:

```go
func isUnalignedStruct(pass *analysis.Pass, n *ast.StructType) (wastedBytes int64, ok bool) {
    st, ok := pass.TypesInfo.TypeOf(n).Underlying().(*types.Struct)
    if !ok || st.NumFields() < 2 {
        return 0, false
    }
    actual := pass.TypesSizes.Sizeof(st)
    fields := make([]*types.Var, st.NumFields())
    for i := range fields {
        fields[i] = st.Field(i)
    }
    sort.SliceStable(fields, func(i, j int) bool {
        return pass.TypesSizes.Alignof(fields[i].Type()) >
            pass.TypesSizes.Alignof(fields[j].Type())
    })
    reordered := pass.TypesSizes.Sizeof(types.NewStruct(fields, nil))
    if reordered >= actual {
        return 0, false
    }
    return actual - reordered, true
}
```

§4's dispatcher calls this as
`if wasted, ok := isUnalignedStruct(pass, node); ok { astutil.Report(pass, node.Pos(), "performance-02", tmpl, wasted) }`
— the prior version of this predicate had the report call inline and a
`Finding.Message` `%s` verb for the struct's name with no code path that
ever derived it (an unresolvable format argument, latent even before this
pass). Fixed by dropping the name from the message rather than fabricating
a derivation; a struct literal's own `*ast.StructType` node does not carry
its declared name (that lives on the enclosing `*ast.TypeSpec`, one level up
the ancestor stack, and anonymous struct types have no name at all) — not
worth adding a second stack-walking helper for a cosmetic label.

Exclusions: structs with <2 fields (no reorder possible); structs where the
sorted layout equals the actual size (already optimal — e.g. all-`int64` or
all-`bool` structs have zero achievable savings).

`Finding.Message`: `"struct field order wastes %d bytes of padding — reorder largest-alignment fields first"`.

`Finding.Severity`: **Warning**. Not `Error` — no correctness break. Not
`Info` — in a long-running data-processing worker, struct instances back every
decoded record and scheduling decision; padding waste compounds directly into
GC pressure and cache-line misses under mixed-input load, a real operational
cost, not a style nit.

---

### performance-03 — preallocate slice/map in a bounded loop

> Source (merged #3 + #11): "Preallocate slices and maps when size is known
> or bounded... A `make([]T, 0)` followed by N appends is N log N copies...
> Appending in a loop without a capacity hint is the common allocator killer."

**Checkable**: the specific pattern `make([]T, 0)` (no cap, or literal `0`
cap) immediately followed by unconditional `append` to that same identifier
inside a loop whose trip count is statically a known collection (slice/array/
map range, or `for i:=0;i<len(x);i++`) is syntactically exact.

**Predicate**:

```go
func isZeroCapMake(info *types.Info, call *ast.CallExpr) (ident string, ok bool) {
    if id, isMake := call.Fun.(*ast.Ident); !isMake || id.Name != "make" {
        return "", false
    }
    if _, isSlice := info.TypeOf(call).(*types.Slice); !isSlice {
        return "", false
    }
    if len(call.Args) == 2 {
        return "", true // make([]T, 0) or make([]T, n) with no cap arg — both risk growth if 0
    }
    if len(call.Args) == 3 {
        if lit, isLit := call.Args[2].(*ast.BasicLit); isLit && lit.Value == "0" {
            return "", true
        }
    }
    return "", false
}

// Then, for each *ast.AssignStmt `x = append(x, ...)` unconditionally
// reachable inside the loop body (not nested under an *ast.IfStmt within
// that body — conditional appends make final size non-obvious):
func isUnconditionalAppendInLoopBody(loopBody *ast.BlockStmt, target string) bool {
    for _, stmt := range loopBody.List {
        assign, ok := stmt.(*ast.AssignStmt)
        if !ok {
            continue
        }
        if lhs, ok := assign.Lhs[0].(*ast.Ident); ok && lhs.Name == target {
            if call, ok := assign.Rhs[0].(*ast.CallExpr); ok {
                if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "append" {
                    return true
                }
            }
        }
    }
    return false
}
```

Exclusions: `make([]T, 0, n)` with any non-`"0"` literal or non-literal
capacity expression (cap hint already present — compliant); `append` nested
inside an `*ast.IfStmt`/`*ast.SwitchStmt` within the loop body (final count
isn't the loop trip count, flagging would false-positive on legitimately
filtered accumulation); loop whose bound isn't a countable collection or
`len()`-derived (e.g., ranging a channel — unknown final size, no fix to
suggest).

`Finding.Message`: `"%s := make([]T, 0) at %s is appended to in a loop bounded by %s known elements — preallocate with make([]T, 0, %s)"`.

`Finding.Severity`: **Warning**. Reallocation-copy churn directly taxes GC
pause time on request-handling hot paths; not `Error` since output is still
correct, not `Info` since the fix is a one-token change with a measurable win.

---

### performance-04 — `strings.Builder`, not `+=`, in a loop

> Source: "Use `strings.Builder` for concatenation in a loop. Not `+`
> (allocates a new string per concat and copies the accumulated bytes each
> time)."

**Checkable**: `+=` on a `string`-typed LHS is unambiguous via
`go/types`; loop membership is unambiguous via ancestor stack.

**Predicate**:

```go
func isStringLoopConcat(info *types.Info, assign *ast.AssignStmt, stack []ast.Node) bool {
    if assign.Tok != token.ADD_ASSIGN || len(assign.Lhs) != 1 {
        return false
    }
    basic, ok := info.TypeOf(assign.Lhs[0]).Underlying().(*types.Basic)
    if !ok || basic.Kind() != types.String {
        return false
    }
    return insideLoop(stack)
}
```

Exclusions: LHS type is not exactly `string` (e.g. numeric `+=`, `[]byte`
append-via-`+=` is not valid Go anyway); `+=` outside any `for`/`range`
(single-shot concatenation is not the O(N²) pattern the rule targets).

`Finding.Message`: `"%s += ... inside a loop at %s allocates a new string every iteration — use strings.Builder with Grow"`.

`Finding.Severity`: **Warning**. O(N²) byte-copy cost under production
message volumes (log line assembly, batch key construction) is a real
latency/GC cost, not just style; not `Error` because correctness is intact
and the loop bound is usually small enough not to be catastrophic on its own.

---

### performance-05 — `strconv` over bare `fmt.Sprintf("%d", n)`

> Source: "Prefer `strconv` over `fmt` for primitive-to-string on hot paths.
> `strconv.Itoa(n)`... skip the reflection and formatting machinery that
> `fmt.Sprintf("%d", n)` spins up."

**Checkable**: scoped tightly to the exact Common-Mistakes-table pattern —
`fmt.Sprintf` called with exactly 2 args, format string is the bare literal
`"%d"`, second arg is an integer type. Wider verb/format matching (`%s`,
`%f`, multi-part templates) is deliberately excluded — see below.

**Predicate**:

```go
func isBareSprintfInt(pass *analysis.Pass, call *ast.CallExpr) bool {
    if !astutil.IsPkgFunc(pass, call, "fmt", "Sprintf") {
        return false
    }
    info := pass.TypesInfo
    if len(call.Args) != 2 {
        return false
    }
    lit, ok := call.Args[0].(*ast.BasicLit)
    if !ok || lit.Kind != token.STRING || lit.Value != `"%d"` {
        return false
    }
    b, ok := info.TypeOf(call.Args[1]).Underlying().(*types.Basic)
    return ok && b.Info()&types.IsInteger != 0
}
```

Exclusion (deliberate, stated explicitly): any format string with additional
literal text (`"item:%d"`) or a non-`%d` verb is **not** flagged. The source
rule only names hot-path cost for `"%d"`; extending the match to `%s`/`%f`/
`%v` would flag the overwhelming majority of idiomatic `fmt.Sprintf` calls in
the codebase (error messages, log lines) with no evidence any given call is
actually hot — that is an unacceptable false-positive rate for a rule whose
own source text (rule #1) says "measure before optimizing." Scoping to the
exact bare-`%d`-on-an-int shape keeps the signal precise.

`Finding.Message`: `"fmt.Sprintf(\"%%d\", %s) at %s — use strconv.Itoa(%s)"`.

`Finding.Severity`: **Info**. No profiling evidence backs "hot path" for any
given call site in a single-file AST pass; flag as a low-confidence
efficiency suggestion, not an actionable defect, per the source's own
measure-first philosophy (`performance-01`).

---

### performance-06 — hoist constant `[]byte(string)` out of a loop

> Source: "Convert a fixed string to `[]byte` once... Every
> `[]byte(string)` conversion allocates and copies."

**Checkable**: a `[]byte(...)` conversion whose argument is a compile-time
constant, located inside a loop, is an unconditional per-iteration
reallocation of an identical value — exact match, no ambiguity.

**Predicate**:

```go
func isConstByteConversionInLoop(info *types.Info, call *ast.CallExpr, stack []ast.Node) bool {
    return isByteSliceConversion(info, call) &&
        isConstantString(info, call.Args[0]) &&
        insideLoop(stack)
}
```

Exclusions: argument is not a compile-time constant (a per-iteration
variable legitimately needs a fresh conversion); conversion sits outside any
loop (one-time cost, not the targeted pattern).

`Finding.Message`: `"[]byte(%s) at %s converts a constant string every loop iteration — hoist to a package-level var and reuse"`.

`Finding.Severity`: **Warning**. Cost scales linearly with loop trip count
on whatever hot path contains it (frequently parser/tokenizer code); fix is
mechanical and risk-free, justifying flagging above `Info`.

---

### performance-07 — write to `io.Writer` directly

> Source: "Write to `io.Writer` directly. `fmt.Fprintf(w, "%d", n)` not
> `w.Write([]byte(fmt.Sprintf("%d", n)))`. The second form builds an
> intermediate string, converts it to a slice, and throws both away."

**Checkable**: the nested call shape `X.Write([]byte(fmt.Sprintf(...)))` is
a fixed 3-level call chain, unambiguous once `X`'s type is confirmed to
satisfy `io.Writer`.

**Predicate**:

```go
func isWriteOfSprintfByteConv(pass *analysis.Pass, call *ast.CallExpr, ioWriter *types.Interface) bool {
    info := pass.TypesInfo
    sel, ok := call.Fun.(*ast.SelectorExpr)
    if !ok || sel.Sel.Name != "Write" {
        return false
    }
    if !types.Implements(info.TypeOf(sel.X), ioWriter) &&
        !types.Implements(types.NewPointer(info.TypeOf(sel.X)), ioWriter) {
        return false
    }
    if len(call.Args) != 1 {
        return false
    }
    conv, ok := call.Args[0].(*ast.CallExpr)
    if !ok || !isByteSliceConversion(info, conv) || len(conv.Args) != 1 {
        return false
    }
    inner, ok := conv.Args[0].(*ast.CallExpr)
    if !ok {
        return false
    }
    return astutil.IsPkgFunc(pass, inner, "fmt", "Sprintf")
}
```

`ioWriter` is obtained once per pass via
`pass.Pkg.Imports()`-independent lookup: `types.NewInterfaceType` built from
the `io.Writer` method set, or simpler — resolve `io.Writer` through
`pass.TypesInfo` on a synthetic reference is unnecessary; use
`analysisutil`-style lookup: load `io` package via `pass.Pkg.Path() != "io"`
guard and `types.Universe`-independent well-known-interface helper (import
`io` in the analyzer package itself and use `reflect.TypeOf((*io.Writer)(nil)).Elem()`-equivalent
via `go/types`: construct once with
`ifc := types.NewInterfaceType([]*types.Func{writeMethod}, nil).Complete()`
where `writeMethod` is built from the exact `Write([]byte) (int, error)`
signature — done once at analyzer init, not per call).

Exclusions: `.Write(...)` call whose receiver is not a `io.Writer`-satisfying
type (e.g. a custom `Write` method unrelated to `io.Writer`, such as a
protobuf builder) — the `types.Implements` check is the exclusion.

`Finding.Message`: `"%s.Write([]byte(fmt.Sprintf(...))) at %s — use fmt.Fprintf(%s, ...) directly"`.

`Finding.Severity`: **Info**. Genuine but small allocation waste (one extra
string + one extra slice per call); no correctness or scale-breaking
consequence, so ranked below the `Warning` rules above.

---

### performance-09 — `reflect` calls inside a loop (hot-path proxy); `any` boxing excluded

> Source: "Avoid `reflect` and `interface{}`/`any` boxing on hot paths.
> Assigning a value type to `any` allocates (boxing the value on the heap).
> Generics or concrete types remove the box."

**Checkable half**: any call into the `reflect` package syntactically
located inside a loop body is checkable — loop membership is the only static
proxy this pass has for "hot path," and it is unambiguous.

**Predicate**:

```go
// isAnyReflectCall reports whether call invokes some function in package
// "reflect", regardless of which one. This is deliberately not
// astutil.IsPkgFunc — that helper matches one named function; this rule
// flags reflect package use in general, so it keeps its own narrow,
// rule-local package match instead of widening the shared helper's contract.
func isAnyReflectCall(pass *analysis.Pass, call *ast.CallExpr) bool {
    sel, ok := call.Fun.(*ast.SelectorExpr)
    if !ok {
        return false
    }
    pkgName, ok := sel.X.(*ast.Ident)
    if !ok {
        return false
    }
    pkgObj, ok := pass.TypesInfo.Uses[pkgName].(*types.PkgName)
    return ok && pkgObj.Imported().Path() == "reflect"
}

func isReflectCallInLoop(pass *analysis.Pass, call *ast.CallExpr, stack []ast.Node) bool {
    if !isAnyReflectCall(pass, call) {
        return false
    }
    return insideLoop(stack)
}
```

`Finding.Message`: `"reflect.%s(...) at %s is called inside a loop body — reflection overhead scales with iteration count; use a concrete type or type parameter"`.

`Finding.Severity`: **Warning**. Reflection cost is real and multiplies by
iteration count, but this pass has no evidence the loop itself is on a
request-serving hot path (could be a one-time startup config walk) — ranked
below `Error`, above `Info` because reflection-in-loop is rarely intentional.

**Excluded half (documented, not silently dropped)**: the "`interface{}`/
`any` boxing" half of this source rule is **excluded from the tool**.
Assigning a concrete value to an `any`/`interface{}`-typed variable or
parameter is one of the most common, often mandatory, idioms in Go —
`fmt.Println(x)`, `errors.New`, `context.WithValue`, any
`map[string]any`/generic-container write, `slog.Any(...)`. A blanket AST
flag on "value assigned to an `any`-typed destination" would fire on a large
fraction of ordinary, correct code with no static signal distinguishing
"this happens 80k times/sec" from "this happens once at startup." Inventing
a heuristic (e.g., "inside a loop" — same proxy as the reflect half) would
still swamp real hot-path findings under log-statement and error-wrapping
noise across the codebase. Excluded per 00-INDEX's explicit permission to
document rather than fabricate a fragile check.

---

### performance-10 — copy a small window before returning; don't reslice-and-return

> Source: "Copy slices to detach backing arrays. `b := make([]T, len(a));
> copy(b, a)` prevents a small slice from keeping a large backing array
> alive. Reslicing a 1M-element slice to return 2 elements pins all 1M in
> memory."

**Checkable, narrowly scoped**: proving "the source slice is large" is
undecidable statically. Scope to the source's own exact example shape: a
function returns a slice expression (`x[low:high]`) taken directly off one
of its own **parameters**, where both bounds are compile-time literals
forming a small fixed window (`≤ 8` elements, matching the reference's `[:2]`
example order of magnitude) — the exact "return `all[:2]`" pattern. Returning
a parameter-derived slice with a non-literal or large bound (e.g. `x[i:]`
pagination, `x[:n]` where `n` is a runtime size) is excluded — that may be
an intentional large-window return where copying would defeat the purpose.

**Predicate**:

```go
func isSmallWindowReslicedReturn(info *types.Info, ret *ast.ReturnStmt, fn *ast.FuncDecl) bool {
    if len(ret.Results) != 1 {
        return false
    }
    se, ok := ret.Results[0].(*ast.SliceExpr)
    if !ok || se.Slice3 {
        return false
    }
    ident, ok := se.X.(*ast.Ident)
    if !ok || !isDeclaredParam(fn, info, info.ObjectOf(ident)) {
        return false
    }
    lowOK := se.Low == nil || isSmallLiteral(se.Low)
    hi, ok := se.High.(*ast.BasicLit)
    if !ok || hi.Kind != token.INT {
        return false
    }
    n, _ := strconv.Atoi(hi.Value)
    return lowOK && n <= 8
}

// isDeclaredParam reports whether obj is one of fn's declared parameters.
// info is threaded through explicitly rather than closed over — this file
// previously referenced an out-of-scope info from the caller, which does
// not compile; declared as an explicit parameter here to close that gap.
func isDeclaredParam(fn *ast.FuncDecl, info *types.Info, obj types.Object) bool {
    for _, p := range fn.Type.Params.List {
        for _, name := range p.Names {
            if info.ObjectOf(name) == obj {
                return true
            }
        }
    }
    return false
}
```

Exclusions: `se.X` is a local variable created via `make`/composite literal
within the same function (the function owns that backing array outright —
no caller-owned pinning risk); `se.High` is absent or a non-literal
expression (unbounded/dynamic window — not the targeted pattern); returned
expression is not itself a `*ast.SliceExpr` (e.g. a separately declared,
already-copied identifier — compliant by construction).

`Finding.Message`: `"return %s[:%d] at %s reslices a parameter without copying — the full backing array stays reachable; copy into a right-sized slice before returning"`.

`Finding.Severity`: **Error**. In a long-running input-processing worker
(reader and dispatcher modules do not restart per item), an unbounded
backing-array retention is unbounded heap growth under sustained traffic —
an operational incident (OOM/eviction), not a tunable efficiency nit.

---

### performance-12 — `b.ResetTimer()` after setup, before the `b.N` loop

> Source: "Use `testing.B`, call `b.ResetTimer()` after setup... A single
> benchmark run is noise, not a measurement." / Common Mistakes: "Not
> calling `b.ResetTimer` after setup" → "`ResetTimer()` then
> `ReportAllocs()` before the loop."

**Checkable half**: `ResetTimer()` presence/absence relative to a `b.N`
loop and prior setup statements is a structural, mechanical check.
`ReportAllocs()` is excluded — see below.

**Predicate**:

```go
func findBenchmarkSetupWithoutReset(fn *ast.FuncDecl, info *types.Info) (setupStmt ast.Node, ok bool) {
    if !strings.HasPrefix(fn.Name.Name, "Benchmark") || len(fn.Type.Params.List) != 1 {
        return nil, false
    }
    param := fn.Type.Params.List[0].Names[0]
    if info.TypeOf(param).String() != "*testing.B" {
        return nil, false
    }
    for i, stmt := range fn.Body.List {
        if isBNLoop(stmt, param.Name) {
            setup := fn.Body.List[:i]
            if len(setup) == 0 {
                return nil, false // nothing before the loop to reset
            }
            for _, s := range setup {
                if callsMethod(s, param.Name, "ResetTimer") {
                    return nil, false // compliant
                }
            }
            return setup[0], true // flag at first setup statement
        }
    }
    return nil, false // no b.N loop found — not this pattern
}

func isBNLoop(stmt ast.Stmt, paramName string) bool {
    fs, ok := stmt.(*ast.ForStmt)
    if !ok {
        return false
    }
    cmp, ok := fs.Cond.(*ast.BinaryExpr)
    if !ok || cmp.Op != token.LSS {
        return false
    }
    sel, ok := cmp.Y.(*ast.SelectorExpr)
    id, idOK := sel.X.(*ast.Ident)
    return ok && idOK && id.Name == paramName && sel.Sel.Name == "N"
}
```

Exclusions: no setup statements before the `b.N` loop (nothing to reset);
`ResetTimer()` called anywhere among the setup statements, regardless of
exact position (compliant); function has no `for i := 0; i < b.N; ...` loop
at all — a table-driven benchmark using `b.Run` per case is a different
shape, deliberately not matched (would need per-subtest analysis and risks
false positives on the outer dispatch function, which legitimately has no
timer to reset).

**Excluded half (documented)**: `b.ReportAllocs()` is advisory, not
correctness-affecting — omitting it means the benchmark output lacks an
extra column, it does not corrupt the ns/op measurement the way a missing
`ResetTimer()` does. Flagging its absence would be a pure style opinion with
no "real consequence" to cite, which this domain file's severity-
justification requirement can't satisfy honestly. Excluded.

`Finding.Message`: `"%s at %s performs setup before the b.N loop without calling b.ResetTimer() — setup cost inflates the reported ns/op"`.

`Finding.Severity`: **Info**. Affects benchmark measurement accuracy only;
zero production consequence.

---

### Excluded rules (documented, not silently dropped)

- **`performance-01`** (measure before optimizing): a methodology statement
  about developer workflow ("did you run `pprof` before writing this code"),
  not a property of the source text in front of the analyzer. No AST
  predicate can observe a prior profiling session. Excluded.
- **`performance-11`** (maps don't shrink after deletes): "high-delete
  churn" is a runtime/traffic property, not a static one. `delete()` calls
  inside a loop are extremely common and correct (e.g., draining a
  work-tracking map) — there is no syntactic signal distinguishing "this map
  grew to 1M and drained to 100" from ordinary bounded cleanup. A heuristic
  keyed on "loop with delete()" would false-positive on the majority of
  correct map-cleanup code. Excluded.
- **`performance-13`** (least mechanism for dependencies): requires a
  judgment call about whether a specific dependency was "necessary" versus
  "convenient," which needs product/architectural context no AST predicate
  over a single package has. This is a code-review-time judgment, not a
  static-analysis-time one. Excluded.

---

## 3. Fixture file spec

Path, canonical, per CONTRACTS' testdata fixture layout:
`internal/tools/testdata/fixtures/audit-performance/rule<NN>/`. One isolated
Go package per checkable rule, package name matching its directory
(`package rule02`, `package rule03`, ...). Each rule directory holds a
`violation.go` with a `// VIOLATION: performance-NN` comment directly above
the offending declaration, and — for every rule with a false-positive risk
worth guarding — a separate `compliant.go` with a `// COMPLIANT: performance-NN`
counterpart. Never a mixed file with both, per CONTRACTS.

**`audit-performance/rule02/violation.go`**
```go
package rule02

// VIOLATION: performance-02
type Counters struct {
	Hits   bool
	Total  int64
	Misses bool
}
```

**`audit-performance/rule02/compliant.go`**
```go
package rule02

// COMPLIANT: performance-02
type CountersOK struct {
	Total  int64
	Hits   bool
	Misses bool
}
```

**`audit-performance/rule03/violation.go`**
```go
package rule03

// VIOLATION: performance-03
func collectSquares(nums []int) []int {
	out := make([]int, 0)
	for _, n := range nums {
		out = append(out, n*n)
	}
	return out
}
```

**`audit-performance/rule03/compliant.go`**
```go
package rule03

// COMPLIANT: performance-03
func collectSquaresOK(nums []int) []int {
	out := make([]int, 0, len(nums))
	for _, n := range nums {
		out = append(out, n*n)
	}
	return out
}
```

**`audit-performance/rule04/violation.go`**
```go
package rule04

// VIOLATION: performance-04
func joinLoose(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p
	}
	return s
}
```

**`audit-performance/rule04/compliant.go`**
```go
package rule04

import "strings"

// COMPLIANT: performance-04
func joinBuilder(parts []string) string {
	var b strings.Builder
	n := 0
	for _, p := range parts {
		n += len(p) // int += inside a loop: not string-typed, must not trigger performance-04
	}
	b.Grow(n)
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}
```

**`audit-performance/rule05/violation.go`**
```go
package rule05

import "fmt"

// VIOLATION: performance-05
func key(id int) string {
	return fmt.Sprintf("%d", id)
}
```

**`audit-performance/rule05/compliant.go`**
```go
package rule05

import "strconv"

// COMPLIANT: performance-05
func keyOK(id int) string {
	return strconv.Itoa(id)
}
```

**`audit-performance/rule06/violation.go`**
```go
package rule06

import "bytes"

// VIOLATION: performance-06
func countNewlines(chunks [][]byte) int {
	total := 0
	for _, c := range chunks {
		total += bytes.Count(c, []byte("\n"))
	}
	return total
}
```

**`audit-performance/rule06/compliant.go`**
```go
package rule06

import "bytes"

var newline = []byte("\n")

// COMPLIANT: performance-06
func countNewlinesOK(chunks [][]byte) int {
	total := 0
	for _, c := range chunks {
		total += bytes.Count(c, newline)
	}
	return total
}
```

**`audit-performance/rule07/violation.go`**
```go
package rule07

import (
	"fmt"
	"io"
)

// VIOLATION: performance-07
func writeCount(w io.Writer, n int) {
	w.Write([]byte(fmt.Sprintf("%d", n)))
}
```

**`audit-performance/rule07/compliant.go`**
```go
package rule07

import (
	"fmt"
	"io"
)

// COMPLIANT: performance-07
func writeCountOK(w io.Writer, n int) {
	fmt.Fprintf(w, "%d", n)
}
```

**`audit-performance/rule09/violation.go`**
```go
package rule09

import "reflect"

// VIOLATION: performance-09
func allEqual(items []int) bool {
	for i := 1; i < len(items); i++ {
		if !reflect.DeepEqual(items[i-1], items[i]) {
			return false
		}
	}
	return true
}
```

**`audit-performance/rule09/compliant.go`**
```go
package rule09

// COMPLIANT: performance-09
func allEqualOK(items []int) bool {
	for i := 1; i < len(items); i++ {
		if items[i-1] != items[i] {
			return false
		}
	}
	return true
}
```

**`audit-performance/rule10/violation.go`**
```go
package rule10

// VIOLATION: performance-10
func topTwo(all []int) []int {
	return all[:2]
}
```

**`audit-performance/rule10/compliant.go`**
```go
package rule10

// COMPLIANT: performance-10
func topTwoOK(all []int) []int {
	out := make([]int, 2)
	copy(out, all[:2])
	return out
}
```

**`audit-performance/rule12/violation_test.go`** and
**`audit-performance/rule12/compliant_test.go`**

Named `_test.go` deliberately — `Benchmark*` functions only exist in test
files. `audit.Run`'s `Tests: true` (see §4) is what makes this rule's own
fixture visible to the pass at all.

```go
package rule12

import (
	"testing"
	"time"
)

// VIOLATION: performance-12
func BenchmarkWithSetup(b *testing.B) {
	data := make([]int, 1000)
	time.Sleep(time.Millisecond) // stand-in for expensive setup
	for i := 0; i < b.N; i++ {
		_ = data[i%len(data)]
	}
}
```

```go
package rule12

import (
	"testing"
	"time"
)

// COMPLIANT: performance-12
func BenchmarkWithSetupOK(b *testing.B) {
	data := make([]int, 1000)
	time.Sleep(time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = data[i%len(data)]
	}
}
```

---

## 4. Tool file spec

`internal/tools/go_audit_performance.go`, package `tools`. Pasted verbatim
from contracts.md's conformance block, substituting `Performance`/
`performance`. No named exceptions apply to this tool (unlike
`go_rename_symbol`/`go_module_risk`) — every field, default, and annotation
below is CONTRACTS' unmodified default.

```go
package tools

import (
    "context"
    "fmt"

    "golang.org/x/tools/go/analysis"
    "github.com/ashwingopalsamy/agentic-go/internal/analysis/performance"
    "github.com/ashwingopalsamy/agentic-go/internal/audit"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditPerformanceInput struct {
    Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
    MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
    MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditPerformanceOutput struct {
    Result finding.AuditResult `json:"result"`
}

func AuditPerformanceHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditPerformanceInput) (*mcp.CallToolResult, AuditPerformanceOutput, error) {
    if err := normalizeAuditPerformanceInput(&in); err != nil {
        return nil, AuditPerformanceOutput{}, fmt.Errorf("validating input: %w", err)
    }
    ws, err := resolveInWorkspace(in.Package)
    if err != nil {
        return nil, AuditPerformanceOutput{}, fmt.Errorf("resolving package: %w", err)
    }
    result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{performance.Analyzer})
    if err != nil {
        return nil, AuditPerformanceOutput{}, fmt.Errorf("running performance audit: %w", err)
    }
    return nil, AuditPerformanceOutput{Result: result}, nil
}

func RegisterAuditPerformance(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "go_audit_performance",
        Description: "Audits a Go package for performance anti-patterns (allocation, struct padding, hot-path reflection) and returns structured findings.",
        Annotations: &mcp.ToolAnnotations{
            ReadOnlyHint:    true,
            DestructiveHint: boolPtr(false),
            IdempotentHint:  true,
            OpenWorldHint:   boolPtr(false),
        },
    }, AuditPerformanceHandler)
}

func normalizeAuditPerformanceInput(in *AuditPerformanceInput) error {
    if in.MaxFindings == 0 {
        in.MaxFindings = 200
    }
    if in.MaxFindings > 1000 {
        in.MaxFindings = 1000
    }
    if in.MinSeverity == "" {
        in.MinSeverity = finding.SeverityInfo
    }
    return nil
}
```

`resolveInWorkspace` is defined once, project-wide, not redeclared here.

`internal/analysis/performance/performance.go`, package `performance`
(never `package analysis`), following contracts.md's analysis-subpackage
template exactly. `Analyzer` is the only exported symbol:

```go
package performance

import (
    "go/ast"

    "golang.org/x/tools/go/analysis"
    "golang.org/x/tools/go/analysis/passes/inspect"
    "golang.org/x/tools/go/ast/inspector"

    "github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
    astutil.RegisterRule("performance-02", "field_alignment", finding.SeverityWarning)
    astutil.RegisterRule("performance-03", "preallocate_bounded_loop", finding.SeverityWarning)
    astutil.RegisterRule("performance-04", "string_builder_loop", finding.SeverityWarning)
    astutil.RegisterRule("performance-05", "strconv_over_sprintf", finding.SeverityInfo)
    astutil.RegisterRule("performance-06", "byte_conversion_in_loop", finding.SeverityWarning)
    astutil.RegisterRule("performance-07", "write_directly", finding.SeverityInfo)
    astutil.RegisterRule("performance-09", "reflect_in_loop", finding.SeverityWarning)
    astutil.RegisterRule("performance-10", "slice_backing_array_return", finding.SeverityError)
    astutil.RegisterRule("performance-12", "benchmark_resettimer", finding.SeverityInfo)
}

var Analyzer = &analysis.Analyzer{
    Name:     "performance",
    Doc:      "flags Go performance anti-patterns: struct padding, unbounded append growth, string concat in loops, avoidable reflection, unsafe slice-backing-array returns, and missing benchmark ResetTimer",
    Run:      run,
    Requires: []*analysis.Analyzer{inspect.Analyzer},
}
func run(pass *analysis.Pass) (interface{}, error) {
    insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
    // WithStack, not Preorder: performance-09 needs the ancestor stack to
    // decide whether a reflect call sits inside a loop body, and
    // performance-10 needs to walk up to the enclosing *ast.FuncDecl from
    // a *ast.ReturnStmt.
    insp.WithStack(
        []ast.Node{
            (*ast.StructType)(nil),
            (*ast.AssignStmt)(nil),
            (*ast.CallExpr)(nil),
            (*ast.ReturnStmt)(nil),
        },
        func(n ast.Node, push bool, stack []ast.Node) bool {
            if !push {
                return true
            }
            switch node := n.(type) {
            case *ast.StructType:
                checkFieldAlignment(pass, node)
            case *ast.AssignStmt:
                checkStringBuilderLoop(pass, node, stack)
            case *ast.CallExpr:
                checkPreallocateBoundedLoop(pass, node, stack)
                checkStrconvOverSprintf(pass, node)
                checkByteConversionInLoop(pass, node, stack)
                checkWriteDirectly(pass, node)
                checkReflectInLoop(pass, node, stack)
                checkBenchmarkResetTimer(pass, node, stack)
            case *ast.ReturnStmt:
                checkSliceBackingArrayReturn(pass, node, stack)
            }
            return true
        },
    )
    return nil, nil
}
```

Each `check<Rule>` above wraps the corresponding §2 predicate and calls
`astutil.Report(pass, pos, "performance-NN", tmpl, args...)` on match — no
explicit severity argument at the call site; severity is resolved from the
`RegisterRule` call above via `astutil.RuleSeverity`. §2's predicates are the
source of truth for the match logic; this dispatcher only wires node type to
predicate.

The corrected `packages.Load` mode — the fix for the `performance-02`
(`pass.TypesSizes.Sizeof`) nil-pointer panic — lives in the shared
`internal/audit/run.go`, not in this file, since `audit.Run` is the single
orchestration entry point for all 7 domains:

- **Before** (this file's prior, buggy inline spec):
  `packages.Load(&packages.Config{Mode: packages.LoadSyntax | packages.NeedTypesInfo, Tests: true, Context: ctx}, pkgPath)`
  — `LoadSyntax` is deprecated and does not set `NeedTypesSizes`, so
  `pass.TypesSizes` is `nil` and `performance-02`'s
  `isUnalignedStruct`/`pass.TypesSizes.Sizeof` call panics at runtime on the
  first struct it inspects.
- **After** (per CONTRACTS' `audit.Run`, mandatory for all 7 domains, not
  performance-specific):
  ```go
  Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
      packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
      packages.NeedTypesSizes | packages.NeedTypesInfo | packages.NeedSyntax,
  Tests: true,
  ```
  `NeedTypesSizes` is what supplies `pass.TypesSizes`, resolving the panic.
  `Tests: true` is retained — `performance-12`'s fixture only exists in a
  `_test.go` file (`audit-performance/rule12/violation_test.go`); without
  `Tests: true`, that rule's own fixture is invisible to the pass and its
  verification test in §5 would be unwritable.

Not cached — matches every other Tier-2/4 analysis tool per
contracts.md's cache TTL table ("every test/analysis tool ... 0 (never
cached)").

---

## 5. Verification

`internal/tools/go_audit_performance_test.go`. Each test loads its rule's
fixture directory under the canonical
`internal/tools/testdata/fixtures/audit-performance/rule<NN>/` (fixing this
file's prior drift — it alone used a flat `fixtures/performance/` path).
Every single-rule test asserts `f.Location.File` against the real
workspace-relative fixture path — never `filepath.Base`, never "at that
line" prose, never omitted — per CONTRACTS' §5 template.

```go
func TestAuditPerformance_Rule02(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule02")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-02", f.Rule)
    assert.Equal(t, finding.SeverityWarning, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule02/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule02_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule02")
    for _, f := range findings {
        assert.NotEqual(t, "performance-02", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule03(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule03")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-03", f.Rule)
    assert.Equal(t, finding.SeverityWarning, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule03/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule03_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule03")
    for _, f := range findings {
        assert.NotEqual(t, "performance-03", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule04(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule04")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-04", f.Rule)
    assert.Equal(t, finding.SeverityWarning, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule04/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule04_CompliantIsSilent(t *testing.T) {
    // joinBuilder's own n += len(p) is the deliberate near-miss: an int
    // accumulator inside a loop, not a string one, must not trigger
    // performance-04.
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule04")
    for _, f := range findings {
        assert.NotEqual(t, "performance-04", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule05(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule05")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-05", f.Rule)
    assert.Equal(t, finding.SeverityInfo, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule05/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule05_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule05")
    for _, f := range findings {
        assert.NotEqual(t, "performance-05", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule06(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule06")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-06", f.Rule)
    assert.Equal(t, finding.SeverityWarning, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule06/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule06_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule06")
    for _, f := range findings {
        assert.NotEqual(t, "performance-06", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule07(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule07")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-07", f.Rule)
    assert.Equal(t, finding.SeverityInfo, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule07/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule07_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule07")
    for _, f := range findings {
        assert.NotEqual(t, "performance-07", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule09(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule09")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-09", f.Rule)
    assert.Equal(t, finding.SeverityWarning, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule09/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule09_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule09")
    for _, f := range findings {
        assert.NotEqual(t, "performance-09", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule10(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule10")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-10", f.Rule)
    assert.Equal(t, finding.SeverityError, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule10/violation.go", f.Location.File)
}

func TestAuditPerformance_Rule10_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule10")
    for _, f := range findings {
        assert.NotEqual(t, "performance-10", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAuditPerformance_Rule12(t *testing.T) {
    // Tests: true (see §4) is required for this fixture to be visible at
    // all — Benchmark* only exists in _test.go files.
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule12")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "performance-12", f.Rule)
    assert.Equal(t, finding.SeverityInfo, f.Severity)
    assert.Equal(t, "fixtures/audit-performance/rule12/violation_test.go", f.Location.File)
}
func TestAuditPerformance_Rule12_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, performance.Analyzer, "audit-performance/rule12")
    for _, f := range findings {
        assert.NotEqual(t, "performance-12", f.Rule, "compliant_test.go must not trigger its own rule")
    }
}

func TestAuditPerformance_TotalRuleCount(t *testing.T) {
    assert.Len(t, astutil.RulesInDomain("performance"), 9)
}
```

No test is written for `performance-01`, `performance-11`, `performance-13`
— they are excluded from the tool per §2, so there is no predicate to verify
and no fixture pair to assert on. This is not a gap: a domain file with
zero excluded rules would need zero exclusion tests too; the requirement is
"one test per checkable rule," satisfied above (9 of 9).

Additional cross-cutting assertion (not per-rule, stated once): running the
full fixture tree end-to-end via `go_audit_performance` produces exactly 9
`Finding`s total (one per violation fixture, zero from any compliant
counterpart) — this catches cross-rule interference (e.g., a predicate
double-firing on another rule's fixture file) that per-rule isolation tests
alone would miss.
