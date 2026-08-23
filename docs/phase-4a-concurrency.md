# phase-4a-concurrency.md — `go_audit_concurrency`

> **Release status:** included in v0.1.0.

Reads `contracts.md` first (canonical `Finding`/`AuditResult`/`Location`/`Severity`,
pass skeleton, 6-step recipe, fixture layout). Reads `phase-4a-index.md` for the required
5-part structure this file follows. The original 18-rule concurrency corpus
is reproduced in this specification; no external reference file is required.
concurrency-19 and concurrency-20 (section 2) are pass-added: they are not among that source
file's 18 numbered rules, and their "Source sentence" below quotes this remediation's own
dispatching rationale instead of the reference file, disclosed explicitly at each one.

This file is self-contained: a fresh implementer needs nothing else from Phase 4.

## Shared helpers

Domain-specific helpers below live in `internal/analysis/concurrency/concurrency.go`, `package
concurrency` — this domain's own analysis subpackage (`contracts.md`'s Naming and file
layout: one subpackage per domain, package name matching the directory, never `package analysis`
— that name self-collides with the `golang.org/x/tools/go/analysis` import). This domain no
longer declares `isPkgDotFunc`: that logic is now centralized in `internal/analysis/astutil/`,
and every call site below and in section 2 uses `astutil.IsPkgFunc` instead. The remaining
helpers are genuinely concurrency-domain logic with no astutil equivalent (no other domain needs
a `go`-statement-subtree scanner or a `.Lock()`/`.RLock()` receiver matcher) and stay declared
here.

```go
// containsGoStmt reports whether body contains a *ast.GoStmt anywhere in its
// subtree. KNOWN LIMITATION: also matches a `go` statement nested inside a
// closure that is merely *defined* (assigned/returned), not spawned, in this
// body — accepted false-positive risk, documented, not engineered around
// (parent-aware walk would add a second inspector pass for a rare case).
func containsGoStmt(body ast.Node) bool {
    found := false
    ast.Inspect(body, func(n ast.Node) bool {
        if _, ok := n.(*ast.GoStmt); ok {
            found = true
            return false
        }
        return true
    })
    return found
}

// lockCallReceiver returns the receiver's source text if stmt is exactly
// `<expr>.Lock()` or `<expr>.RLock()`, else "".
func lockCallReceiver(stmt ast.Stmt) (recv string, method string) {
    es, ok := stmt.(*ast.ExprStmt)
    if !ok {
        return "", ""
    }
    call, ok := es.X.(*ast.CallExpr)
    if !ok || len(call.Args) != 0 {
        return "", ""
    }
    sel, ok := call.Fun.(*ast.SelectorExpr)
    if !ok {
        return "", ""
    }
    if sel.Sel.Name != "Lock" && sel.Sel.Name != "RLock" {
        return "", ""
    }
    return types.ExprString(sel.X), sel.Sel.Name
}
```

This domain no longer declares `typeString`: duplicated logic (the identical body also lived in
`phase-4a-errors.md` as `resolvedTypeString`) is now centralized as `astutil.TypeString`, and every call
site below uses it.

Rule-specific helpers are shown inline in each rule's predicate section below (they're only used
by that one rule and adding them here would force readers to cross-reference).

---

## 1. Rules table

| Rule ID | Source rule (paraphrased) | Status |
|---|---|---|
| concurrency-01 | Never fire-and-forget a goroutine | **Disabled (v0.1.0; calibrated)** |
| concurrency-02 | Provide Stop/Close/Shutdown for a type that spawns goroutines | **Disabled (v0.1.0; calibrated)** |
| concurrency-03 | Channel buffer size must be 0 or 1, else a justifying comment | **Disabled (v0.1.0; calibrated)** |
| concurrency-04 | Declare channel direction (`chan<-`/`<-chan`) in signatures | Implemented |
| concurrency-05 | No `XxxAsync` functions that fire an internal goroutine | Implemented |
| concurrency-06 | Never `context.Background()`/`context.TODO()` inside a spawned goroutine | **Disabled (v0.1.0; calibrated)** |
| concurrency-07 | `defer mu.Unlock()` immediately after `mu.Lock()`, nothing between | **Disabled (v0.1.0; calibrated)** |
| concurrency-08 | Never embed `sync.Mutex`/`sync.RWMutex`/`sync.WaitGroup` in a struct | Implemented |
| concurrency-09 | Never spawn goroutines in `init()` | Implemented |
| concurrency-10 | Document whether a type is concurrency-safe | Implemented (narrowed — see below) |
| concurrency-11 | Prefer context cancellation over `done` channels for stop signalling | **Excluded** |
| concurrency-12 | Use `errgroup.Group` for coordinated goroutines that must all succeed | Implemented (detects the hand-rolled anti-pattern) |
| concurrency-13 | Run `go test -race` in CI; never merge a race | **Excluded** |
| concurrency-14 | `sync.Pool`/`sync.Once` usage discipline; reset pooled items before `Put` | **Disabled (v0.1.0; calibrated)** |
| concurrency-15 | `sync.Map`/`sync/atomic` niches; compound state still needs a mutex | **Disabled (v0.1.0; calibrated)** |
| concurrency-16 | Prefer `sync.Mutex` over `sync.RWMutex` unless reads dominate + long sections | **Excluded** |
| concurrency-17 | Multi-lock acquisition: consistent global order, documented in a comment | **Disabled (v0.1.0; calibrated)** |
| concurrency-18 | Stop local `Ticker`s; never `time.After` in a `select` loop | Implemented (narrowed — see below) |
| concurrency-19 | Loop variable captured by reference in a go/defer closure | Implemented (pass-added, not in source's 18 — version-gated, pre-1.22 targets only) |
| concurrency-20 | `defer` directly inside a loop, unscoped | **Disabled (v0.1.0; calibrated)** |

8 active, 9 disabled after external calibration, 3 excluded. Disabled rules
retain their predicates and detailed fixture/design material below for future
redesign, but emit no findings and are excluded from the active rule resource.
Excluded rules get no `Finding.Rule` value, no fixture, no test — see each
rule's "Why excluded" note in section 2.

---

## 2. Per-rule AST pattern

<a id="concurrency-01"></a>
### concurrency-01 — fire-and-forget goroutine

**v0.1.0 status:** Disabled after external calibration. The predicate and
fixture below are retained as future redesign documentation; this rule emits
no findings and is not part of the active rule resource.

**Source sentence:** "Never fire-and-forget a goroutine. Every goroutine needs a clear owner, a stop signal (context cancellation or done channel), and a waiter that confirms exit."

**Why checkable:** A `go` statement is a fixed syntactic node. "Has an owner/stop/waiter" reduces to three detectable co-occurring patterns in the enclosing block: a `sync.WaitGroup.Add` call, an `errgroup` `.Go` call, or (for inline literals) a `select` on a `Done()`-shaped channel inside the literal body.

**Node type(s):** `*ast.GoStmt`, its enclosing `*ast.BlockStmt`.

**Predicate:**
```go
func isFireAndForget(pass *analysis.Pass, gs *ast.GoStmt, enclosing *ast.BlockStmt) bool {
    for _, stmt := range enclosing.List {
        call, ok := astutil.ExprStmtCall(stmt)
        if !ok {
            continue
        }
        sel, ok := call.Fun.(*ast.SelectorExpr)
        if !ok {
            continue
        }
        if sel.Sel.Name == "Add" && strings.Contains(astutil.TypeString(pass, sel.X), "sync.WaitGroup") {
            return false
        }
        if sel.Sel.Name == "Go" && strings.Contains(astutil.TypeString(pass, sel.X), "errgroup.Group") {
            return false
        }
    }
    if lit, ok := gs.Call.Fun.(*ast.FuncLit); ok {
        return !hasDoneSelect(lit.Body)
    }
    for _, arg := range gs.Call.Args {
        if astutil.TypeString(pass, arg) == "context.Context" {
            return false
        }
    }
    return true
}

func hasDoneSelect(body ast.Node) bool {
    found := false
    ast.Inspect(body, func(n ast.Node) bool {
        cc, ok := n.(*ast.CommClause)
        if !ok || cc.Comm == nil {
            return true
        }
        var callExpr *ast.CallExpr
        switch comm := cc.Comm.(type) {
        case *ast.ExprStmt:
            if ue, ok := comm.X.(*ast.UnaryExpr); ok && ue.Op == token.ARROW {
                callExpr, _ = ue.X.(*ast.CallExpr)
            }
        case *ast.AssignStmt:
            if len(comm.Rhs) == 1 {
                if ue, ok := comm.Rhs[0].(*ast.UnaryExpr); ok && ue.Op == token.ARROW {
                    callExpr, _ = ue.X.(*ast.CallExpr)
                }
            }
        }
        if callExpr != nil {
            if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Done" {
                found = true
            }
        }
        return true
    })
    return found
}
```

**Exclusions:** skip `*ast.GoStmt` in `_test.go` files (test-spawned goroutines commonly rely on `t.Cleanup`, a distinct idiom out of scope here).

**Finding.Message:** `"goroutine spawned at %s has no owner, stop signal, or waiter — wrap it in a Start/Stop-owned worker, errgroup.Group, or a WaitGroup"`

**Finding.Severity:** `SeverityError` — this is the file's own stated root cause of production incidents (silent work loss on panic); matches go-security.md's zero-tolerance stance on unrecoverable background work.

---

<a id="concurrency-02"></a>
### concurrency-02 — missing Stop/Close/Shutdown

**v0.1.0 status:** Disabled after external calibration. Operation-scoped
goroutines commonly express ownership through contexts, result channels,
streams, returned handles, or enclosing locks; the receiver method set is not
a reliable lifecycle boundary. The retained predicate is redesign material and
emits no finding.

**Source sentence:** "Provide `Stop`/`Close`/`Shutdown` for any type that spawns background goroutines. Callers must be able to shut it down."

**Why checkable:** Two-pass, purely syntactic: (1) collect every named type's method set by receiver type name, (2) for every method whose body contains a `*ast.GoStmt`, check the same type's method set for `Stop`/`Close`/`Shutdown`.

**Node type(s):** `*ast.FuncDecl` (with receiver).

**Predicate:**
```go
func receiverTypeName(expr ast.Expr) string {
    if star, ok := expr.(*ast.StarExpr); ok {
        expr = star.X
    }
    if id, ok := expr.(*ast.Ident); ok {
        return id.Name
    }
    return ""
}
// pass 1 (whole package, before reporting):
methodSet := map[string]map[string]bool{}
spawningMethods := map[string]*ast.FuncDecl{} // typeName -> first offending method
insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
    fd := n.(*ast.FuncDecl)
    if fd.Recv == nil || len(fd.Recv.List) == 0 {
        return
    }
    tn := receiverTypeName(fd.Recv.List[0].Type)
    if methodSet[tn] == nil {
        methodSet[tn] = map[string]bool{}
    }
    methodSet[tn][fd.Name.Name] = true
    if fd.Body != nil && containsGoStmt(fd.Body) {
        if _, seen := spawningMethods[tn]; !seen {
            spawningMethods[tn] = fd
        }
    }
})
// pass 2:
for tn, fd := range spawningMethods {
    m := methodSet[tn]
    if !m["Stop"] && !m["Close"] && !m["Shutdown"] {
        // report Finding at fd.Pos()
    }
}
```

**Exclusions:** skip types declared in `_test.go` files.

**Finding.Message:** `"type %s spawns a background goroutine in %s but declares no Stop/Close/Shutdown method"`

**Finding.Severity:** `SeverityError` — a caller with no shutdown hook cannot drain in-flight work on rollout; identical failure mode to the file's own "Production Note" incident (SIGTERM mid-write, jobs vanish).

---

<a id="concurrency-03"></a>
### concurrency-03 — unjustified channel buffer size

**Source sentence:** "Channel buffer size must be 0 or 1. Any other size requires a comment justifying why."

**Why checkable:** `make(chan T, N)` is a fixed call shape; `N` as a literal is staticaly evaluable; comment presence is queryable via `ast.NewCommentMap`.

**Node type(s):** `*ast.CallExpr` (`make`), `*ast.ChanType`, `*ast.BasicLit`.

**Predicate:**
```go
func unjustifiedBuffer(call *ast.CallExpr, cmap ast.CommentMap, stmt ast.Node) bool {
    id, ok := call.Fun.(*ast.Ident)
    if !ok || id.Name != "make" || len(call.Args) < 2 {
        return false
    }
    if _, ok := call.Args[0].(*ast.ChanType); !ok {
        return false
    }
    lit, ok := call.Args[1].(*ast.BasicLit)
    if !ok || lit.Kind != token.INT {
        return false // non-literal size: not statically evaluable, skip
    }
    n, err := strconv.Atoi(lit.Value)
    if err != nil || n <= 1 {
        return false
    }
    return len(cmap[stmt]) == 0
}
```

**Exclusions:** non-literal size expressions (identifiers, consts, arithmetic) are skipped — cannot be evaluated without a constant-folding pass, and flagging them risks false positives on deliberately-configured, already-justified pool sizes.

**Finding.Message:** `"channel buffer size %d at %s exceeds 1 with no justifying comment"`

**Finding.Severity:** `SeverityWarning` — masks backpressure and grows memory under load per the source, but is a degradation over time, not an immediate crash (contrast with concurrency-01/06/08).

---

<a id="concurrency-04"></a>
### concurrency-04 — undirected channel in a signature

**Source sentence:** "Declare channel direction in signatures: `chan<- T` for send-only, `<-chan T` for receive-only. Direction turns runtime panics into compile-time errors."

**Why checkable:** `ast.ChanType.Dir` is a bitmask field (`ast.SEND`, `ast.RECV`, or both) directly on the node.

**Node type(s):** `*ast.FuncType` params/results field list, `*ast.ChanType`.

**Predicate:**
```go
func isUndirectedSignatureChan(field *ast.Field) bool {
    ct, ok := field.Type.(*ast.ChanType)
    if !ok {
        return false
    }
    return ct.Dir == (ast.SEND | ast.RECV)
}
```
Applied only to `fd.Type.Params.List` and `fd.Type.Results.List` for every `*ast.FuncDecl`/`*ast.FuncLit` — never to local `var`/`make` declarations, which legitimately need a bidirectional channel before handing off a restricted view.

**Exclusions:** channel fields inside a `struct` type or local variable declarations are out of scope — the source rule is specifically about signatures. Declarations in `_test.go` are excluded after calibration found ordinary test coordination rather than public API contracts.

**Finding.Message:** `"parameter/return channel at %s has no direction (chan T) — declare chan<- T or <-chan T"`

**Finding.Severity:** `SeverityWarning` — a real defect-prevention mechanism (wrong-direction use fails only at runtime with a panic or deadlock otherwise), but the current code is not itself broken, so it ranks below rules that flag an active hazard.

---

<a id="concurrency-05"></a>
### concurrency-05 — `XxxAsync` internal-goroutine wrapper

**Source sentence:** "Prefer synchronous functions. No `ProcessAsync` that fires a goroutine internally. Let the caller add concurrency with `errgroup` or a worker pool."

**Why checkable:** Name-suffix match plus goroutine-in-body is a precise, low-noise heuristic; excluding functions that return a caller-awaitable channel keeps false positives low.

**Node type(s):** `*ast.FuncDecl`.

**Predicate:**
```go
func isAsyncWrapper(fd *ast.FuncDecl) bool {
    if !strings.HasSuffix(fd.Name.Name, "Async") || fd.Body == nil {
        return false
    }
    if !containsGoStmt(fd.Body) {
        return false
    }
    if fd.Type.Results != nil {
        for _, r := range fd.Type.Results.List {
            if _, ok := r.Type.(*ast.ChanType); ok {
                return false // caller gets a channel to await — not fire-and-forget
            }
        }
    }
    return true
}
```

**Exclusions:** functions returning a channel type (caller-awaitable) are exempt — they hand the caller a waiter, satisfying the rule's actual intent even though the name matches. `_test.go` functions are excluded because test helper names commonly use an `Async` suffix without defining a production API.

**Finding.Message:** `"%s fires a goroutine internally instead of letting the caller add concurrency via errgroup or a worker pool"`

**Finding.Severity:** `SeverityWarning` — a design smell that tends to *reintroduce* concurrency-01's risk one layer down, but is not itself a proven hazard until inspected further.

---

<a id="concurrency-06"></a>
### concurrency-06 — `context.Background()`/`context.TODO()` inside a spawned goroutine

**v0.1.0 status:** Disabled after external calibration found standalone benchmark workers whose
fixed workloads intentionally use root contexts. The predicate and fixture below remain as future
redesign documentation; this rule emits no findings and is not part of the active rule resource.

**Source sentence:** "Propagate `context.Context` across goroutine boundaries. Never create `context.Background()` inside a spawned goroutine; use the parent context or a derived one."

**Why checkable:** `context.Background`/`context.TODO` are two fixed, unambiguous package-qualified calls resolvable via `pass.TypesInfo.Uses`.

**Node type(s):** `*ast.GoStmt` with `*ast.FuncLit` callee, `*ast.CallExpr`, `*ast.SelectorExpr`.

**Predicate:**
```go
func hasBackgroundInGoroutine(pass *analysis.Pass, lit *ast.FuncLit) bool {
    found := false
    ast.Inspect(lit.Body, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }
        if astutil.IsPkgFunc(pass, call, "context", "Background") || astutil.IsPkgFunc(pass, call, "context", "TODO") {
            found = true
        }
        return true
    })
    return found
}
```

**Exclusions:** skip when the enclosing top-level function is `main` (`fd.Recv == nil && fd.Name.Name == "main"`) — this is true program bootstrap with no parent context to derive from, the one legitimate `context.Background()` call site in the whole codebase (matches the "graceful shutdown" correct example in the source, which calls it exactly once in `main`).

**Finding.Message:** `"goroutine at %s calls context.Background()/context.TODO() instead of deriving from the parent context — orphans work after client disconnect"`

**Finding.Severity:** `SeverityError` — directly the class of bug go-security.md calls out by name (orphaned outbound call after disconnect, sub-second SLA violated silently).

---

<a id="concurrency-07"></a>
### concurrency-07 — gap between `Lock()` and `defer Unlock()`

**Source sentence:** "`defer mu.Unlock()` immediately after `mu.Lock()`, with nothing between. Code between lock and defer is a panic window that leaves the mutex locked."

**Why checkable:** Statement adjacency within a `*ast.BlockStmt.List` is directly inspectable; `Lock`/`RLock` and `Unlock`/`RUnlock` are the only method names in play, and the receiver's static type can be checked against `sync.Mutex`/`sync.RWMutex` to avoid matching unrelated `Lock()` methods.

**Node type(s):** `*ast.BlockStmt`.

**Predicate:**
```go
func lockDeferGapViolations(pass *analysis.Pass, block *ast.BlockStmt) []ast.Stmt {
    var bad []ast.Stmt
    for i, stmt := range block.List {
        recv, method := lockCallReceiver(stmt)
        if recv == "" {
            continue
        }
        t := astutil.TypeString(pass, mustCallRecv(stmt))
        if !strings.Contains(t, "sync.Mutex") && !strings.Contains(t, "sync.RWMutex") {
            continue // not a sync mutex — avoid matching unrelated Lock() methods
        }
        wantUnlock := "Unlock"
        if method == "RLock" {
            wantUnlock = "RUnlock"
        }
        if i+1 >= len(block.List) {
            bad = append(bad, stmt)
            continue
        }
        def, ok := block.List[i+1].(*ast.DeferStmt)
        if !ok || !isUnlockCallOn(def.Call, recv, wantUnlock) {
            bad = append(bad, stmt)
        }
    }
    return bad
}

func isUnlockCallOn(call *ast.CallExpr, recv, method string) bool {
    sel, ok := call.Fun.(*ast.SelectorExpr)
    return ok && sel.Sel.Name == method && types.ExprString(sel.X) == recv
}
```

**Exclusions:** none beyond the type-check above — the type check itself is the false-positive guard (a custom type that happens to define `Lock`/`Unlock` for an unrelated purpose is not flagged).

**Finding.Message:** `"%s.Lock() at %s is not immediately followed by defer %s.Unlock() — code between lock and defer is a panic window"`

**Finding.Severity:** `SeverityError` — a panic in that window leaves the mutex permanently locked, deadlocking every future caller; production-fatal, not a style nit.

---

<a id="concurrency-08"></a>
### concurrency-08 — embedded `sync.Mutex`/`sync.RWMutex`/`sync.WaitGroup`

**Source sentence:** "Never embed `sync.Mutex` or `sync.WaitGroup` in a struct. Embedding promotes `Lock`/`Unlock` to the public API. Use a named field: `mu sync.Mutex`."

**Why checkable (narrowed):** Embedded fields are syntactically distinguished by `ast.Field.Names == nil`; the field's static type is directly resolvable. v0.1.0 reports only exported named struct types, because only those types promote the synchronization methods into a public API.

**Node type(s):** `*ast.StructType`, `*ast.Field`.

**Predicate:**
```go
func embeddedSyncPrimitive(pass *analysis.Pass, st *ast.StructType) []*ast.Field {
    var bad []*ast.Field
    for _, f := range st.Fields.List {
        if len(f.Names) != 0 {
            continue // has a name: not an embedded field
        }
        switch astutil.TypeString(pass, f.Type) {
        case "sync.Mutex", "sync.RWMutex", "sync.WaitGroup",
            "*sync.Mutex", "*sync.RWMutex", "*sync.WaitGroup":
            bad = append(bad, f)
        }
    }
    return bad
}
```

**Exclusions:** unexported and anonymous structs are excluded. An exported struct whose only field is the embedded synchronization primitive is also excluded: that exact one-field shape is an intentional lock-wrapper API, not accidental promotion.

**Finding.Message:** `"exported struct %s embeds %s anonymously at %s, promoting Lock/Unlock (or Add/Done/Wait) to its public API — use a named field instead"`

**Finding.Severity:** `SeverityError` — an irreversible API-contract leak once published; removing the promoted methods later is a breaking change for every caller.

---

<a id="concurrency-09"></a>
### concurrency-09 — goroutine in `init()`

**Source sentence:** "Never spawn goroutines in `init()`. Startup ordering is implicit and fragile."

**Why checkable:** `init` is a reserved, unambiguous, receiver-less function name.

**Node type(s):** `*ast.FuncDecl`.

**Predicate:**
```go
func isInitWithGoroutine(fd *ast.FuncDecl) bool {
    return fd.Recv == nil && fd.Name.Name == "init" && fd.Body != nil && containsGoStmt(fd.Body)
}
```

**Exclusions:** none needed — `init()` has no legitimate parameterization to special-case.

**Finding.Message:** `"init() at %s spawns a goroutine — startup ordering becomes implicit and fragile"`

**Finding.Severity:** `SeverityWarning` — the hazard is conditional (only manifests under specific init-order interleavings across files/packages), not a guaranteed failure like concurrency-01/06/08.

---

<a id="concurrency-10"></a>
### concurrency-10 — undocumented concurrency safety (narrowed)

**Source sentence:** "Document whether a type is concurrency-safe, and how: mutex, channels, or immutable-after-construction. Absent a statement, callers must assume it is not safe."

**Why checkable (narrowed):** Determining whether an *arbitrary* type needs a concurrency-safety statement is a judgment call (does it have shared mutable state at all, is it ever accessed from >1 goroutine) that AST alone cannot answer. The rule is narrowed to the syntactically-detectable subset: exported struct types that **already contain** a named `sync.Mutex`/`sync.RWMutex`/`sync.WaitGroup` field (i.e., the type itself signals it's concurrency-relevant) and whose doc comment says nothing about safety. This narrowing is documented, not silent.

**Node type(s):** `*ast.GenDecl` → `*ast.TypeSpec` → `*ast.StructType`.

**Predicate:**
```go
func structHasNamedSyncField(pass *analysis.Pass, st *ast.StructType) bool {
    for _, f := range st.Fields.List {
        if len(f.Names) == 0 {
            continue // embedded case is concurrency-08's job
        }
        s := astutil.TypeString(pass, f.Type)
        if strings.Contains(s, "sync.Mutex") || strings.Contains(s, "sync.RWMutex") || strings.Contains(s, "sync.WaitGroup") {
            return true
        }
    }
    return false
}

func docMentionsSafety(doc *ast.CommentGroup) bool {
    if doc == nil {
        return false
    }
    text := strings.ToLower(doc.Text())
    for _, kw := range []string{
        "safe for concurrent", "safe to use concurrent", "safe to call concurrent",
        "not safe for concurrent", "cannot be called concurrent", "must not be called concurrent",
        "goroutine-safe", "goroutine safe", "concurrency-safe", "concurrency safe",
        "thread-safe", "thread safe",
    } {
        if strings.Contains(text, kw) {
            return true
        }
    }
    return false
}
```
Flag when: `typeSpec.Name.IsExported()`, `structHasNamedSyncField` is true, and `!docMentionsSafety(genDecl.Doc)`.

**Exclusions:** unexported types, exported types with no sync field, `_test.go`, files ending `_testing.go`, and support code under `test`, `testing`, `testutil`, or `testutils` path segments. External calibration found those support APIs to be style noise rather than production concurrency contracts.

**Finding.Message:** `"exported type %s at %s has synchronization fields but its doc comment does not state whether it is safe for concurrent use"`

**Finding.Severity:** `SeverityInfo` — a documentation gap, not a functional defect; matches this project's own severity rubric ("a naming-style nit is SeverityInfo").

---

<a id="concurrency-11"></a>
### concurrency-11 — EXCLUDED: prefer context cancellation over `done` channels

**Source sentence:** "Prefer context cancellation over `done` channels for stop signalling. Reserve `done chan` for when you need to send a value, not just a signal."

**Why excluded:** Whether a given `chan struct{}` in a specific type is "just a signal" (should be `ctx.Done()`) or "needs to send a value" (legitimately a `done chan`) depends on the surrounding design intent and whether a `context.Context` is even available in that type's construction path — neither is a syntactic property. A heuristic like "flag any `chan struct{}` field when the struct also has no `ctx` field" would false-positive on every legitimate case where context isn't threaded through by design (e.g., a low-level primitive deliberately built without a context dependency). No AST predicate here clears the false-positive bar; excluded per the non-negotiable-rigor requirement's own escape clause.

---

<a id="concurrency-12"></a>
### concurrency-12 — hand-rolled `WaitGroup` + error-channel coordination (use `errgroup.Group`)

**Source sentence:** "Use `errgroup.Group` for coordinated goroutines that must all succeed. It propagates the first error and cancels the group on failure."

**Why checkable:** The anti-pattern the source itself illustrates (Critical Trap #3) has a fixed structural signature: a `sync.WaitGroup`-typed local, a `chan error` used to collect per-goroutine results, and 2+ `go` statements in the same block — exactly the shape `errgroup.Group` replaces.

**Node type(s):** `*ast.BlockStmt` (function or literal body).

**Predicate:**
```go
func manualWaitGroupErrChanPattern(pass *analysis.Pass, body *ast.BlockStmt) (found bool, goCount int) {
    hasWG, hasErrChan := false, false
    ast.Inspect(body, func(n ast.Node) bool {
        switch v := n.(type) {
        case *ast.GoStmt:
            goCount++
        case *ast.ValueSpec:
            for _, name := range v.Names {
                if strings.Contains(astutil.TypeString(pass, name), "sync.WaitGroup") {
                    hasWG = true
                }
            }
        case *ast.CallExpr:
            id, ok := v.Fun.(*ast.Ident)
            if ok && id.Name == "make" && len(v.Args) >= 1 {
                if ct, ok := v.Args[0].(*ast.ChanType); ok && astutil.TypeString(pass, ct.Value) == "error" {
                    hasErrChan = true
                }
            }
        }
        return true
    })
    return hasWG && hasErrChan && goCount >= 2, goCount
}
```

**Exclusions:** requires *both* a `sync.WaitGroup` local **and** a `chan error` in the *same* block — a `WaitGroup` used alone (no error propagation attempted) or an error channel fed by a single goroutine (no coordination problem to speak of) do not match. `_test.go` is excluded after calibration found ordinary test concurrency rather than a production coordination design.

**Finding.Message:** `"function at %s hand-rolls WaitGroup + error-channel coordination across %d goroutines — use errgroup.Group to propagate the first error and cancel the rest"`

**Finding.Severity:** `SeverityWarning` — the general pattern is a maintainability/consistency issue; the source's *specific* unbuffered-channel variant deadlocks (Critical Trap #3), but proving the channel is unbuffered and that `Wait()` precedes the read requires statement-order analysis this predicate does not attempt, so severity is set for the general case, not the worst case.

---

<a id="concurrency-13"></a>
### concurrency-13 — EXCLUDED: `go test -race` in CI

**Source sentence:** "Run `go test -race` in CI and never merge code that races under it. The race detector is the enforcement behind every rule in this file; a data race is undefined behavior, not a flake test to investigate later."

**Why excluded:** This is a CI/process policy, not a property of any single Go source file — there is no `ast.Node` to inspect. It is already covered by `contracts.md`'s CI contract (`go test -race ./...` as mandatory step 2, matrix-gated, no skip flags). Restating it as an AST rule would require re-implementing the race detector itself, which is explicitly out of scope for a `go/analysis` pass.

---

<a id="concurrency-14"></a>
### concurrency-14 — `sync.Pool.Put` without a preceding `Reset()` (narrowed)

**Source sentence:** "Reset pooled items to a clean state before `Put` (a `*bytes.Buffer` needs `Reset()`), and do not pool items you cannot fully reset or that still hold references to caller-owned data."

**Why checkable (narrowed):** "Use `sync.Pool`/`sync.Once` to cut GC pressure/for lazy init" is a positive usage encouragement — absence of pooling is not a flaggable defect. The syntactically-checkable slice is the *reset-before-Put* precondition, and only for types that actually expose a `Reset()` method (so "cannot fully reset" cases, which have no such method, are correctly never flagged).

**Cross-reference:** `phase-4a-performance.md`'s source rule #8 (`*bytes.Buffer.Put` without `.Reset()`) was excised there as a duplicate of this rule — `concurrency-14` is the general form (any pooled type exposing `Reset()`, not just `*bytes.Buffer`) and is the sole owner of this check; `performance-08` is not reused as a rule ID.

**Node type(s):** `*ast.BlockStmt`, `*ast.CallExpr` (`.Put(...)`).

**Predicate:**
```go
func putWithoutReset(pass *analysis.Pass, block *ast.BlockStmt) []*ast.CallExpr {
    var bad []*ast.CallExpr
    for i, stmt := range block.List {
        call, ok := astutil.ExprStmtCall(stmt)
        if !ok {
            continue
        }
        sel, ok := call.Fun.(*ast.SelectorExpr)
        if !ok || sel.Sel.Name != "Put" || len(call.Args) == 0 {
            continue
        }
        if !strings.Contains(astutil.TypeString(pass, sel.X), "sync.Pool") {
            continue
        }
        arg := call.Args[0]
        argType := pass.TypesInfo.TypeOf(arg)
        if argType == nil || !hasResetMethod(argType) {
            continue // no Reset() method: "cannot fully reset" case, not flaggable
        }
        argText := types.ExprString(arg)
        if i == 0 || !precedingCallIsReset(block.List[i-1], argText) {
            bad = append(bad, call)
        }
    }
    return bad
}

func hasResetMethod(t types.Type) bool {
    ms := types.NewMethodSet(t)
    for i := 0; i < ms.Len(); i++ {
        if ms.At(i).Obj().Name() == "Reset" {
            return true
        }
    }
    return false
}

func precedingCallIsReset(stmt ast.Stmt, argText string) bool {
    call, ok := astutil.ExprStmtCall(stmt)
    if !ok {
        return false
    }
    sel, ok := call.Fun.(*ast.SelectorExpr)
    return ok && sel.Sel.Name == "Reset" && types.ExprString(sel.X) == argText
}
```

**Exclusions:** pooled types with no `Reset()` method in their method set are skipped entirely (the source rule's "do not pool items you cannot fully reset" branch is a design decision about *whether to pool at all*, not a Put-site defect — excluded from this predicate, not silently dropped: stated here).

**Finding.Message:** `"sync.Pool.Put(%s) at %s has no preceding Reset() call — a pooled item may leak stale data or caller-owned references to the next Get()"`

**Finding.Severity:** `SeverityError` — a shared-server data leak across requests can expose
private payloads to the wrong caller; not a style issue.

---

<a id="concurrency-15"></a>
### concurrency-15 — compound state mutated via multiple independent atomics (narrowed)

**v0.1.0 status:** Disabled after external calibration. Multiple atomic fields
were repeatedly independent counters, metrics, snapshots, and flags rather
than one compound invariant. Syntax cannot establish the missing invariant, so
the retained predicate emits no finding.

**Source sentence:** "Reach for `sync.Map` and `sync/atomic` only in their niches... Use `atomic.Int64`, `atomic.Bool`, and `atomic.Pointer`... for a single hot counter, flag, or pointer without a mutex; compound state still needs a mutex."

**Why checkable (narrowed):** Whether a given `sync.Map` use case is truly "append-only" or "disjoint key sets" (the source's carve-out) is a runtime-access-pattern judgment call, not a syntactic property — excluded. The other half — "compound state still needs a mutex" — has a concrete syntactic signature: a struct with ≥2 independent `atomic.*`-typed fields, and a single method that mutates ≥2 of them with no mutex anywhere in that method.

**Node type(s):** `*ast.StructType`, `*ast.FuncDecl` (method).

**Predicate:**
```go
func atomicFields(pass *analysis.Pass, st *ast.StructType) []*ast.Field {
    var out []*ast.Field
    for _, f := range st.Fields.List {
        if len(f.Names) == 0 {
            continue
        }
        if strings.HasPrefix(astutil.TypeString(pass, f.Type), "sync/atomic.") {
            out = append(out, f)
        }
    }
    return out
}

func methodMutatesMultipleAtomicsWithoutMutex(pass *analysis.Pass, fd *ast.FuncDecl, fields []*ast.Field) bool {
    touched := map[string]bool{}
    hasLock := false
    ast.Inspect(fd.Body, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }
        sel, ok := call.Fun.(*ast.SelectorExpr)
        if !ok {
            return true
        }
        if sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock" {
            hasLock = true
        }
        fieldSel, ok := sel.X.(*ast.SelectorExpr)
        if !ok {
            return true
        }
        mutators := map[string]bool{"Store": true, "Add": true, "Swap": true, "CompareAndSwap": true}
        if !mutators[sel.Sel.Name] {
            return true
        }
        for _, f := range fields {
            if len(f.Names) > 0 && fieldSel.Sel.Name == f.Names[0].Name {
                touched[f.Names[0].Name] = true
            }
        }
        return true
    })
    return !hasLock && len(touched) >= 2
}
```

**Exclusions:** the `sync.Map` access-pattern half of the source rule is excluded for the reason above. Methods that only mutate a *single* atomic field are never flagged — that is the source rule's explicitly correct usage.

**Finding.Message:** `"method %s at %s mutates %d independent atomic fields with no mutex — compound state needs a mutex, not multiple atomics"`

**Finding.Severity:** `SeverityWarning` — produces a torn/inconsistent read between the independent updates, a real correctness bug under concurrency, but not an unrecoverable crash — one tier below concurrency-01/06/08.

---

<a id="concurrency-17"></a>
### concurrency-17 — multi-lock acquisition without an ordering comment (narrowed)

**v0.1.0 status:** Disabled after external calibration. The predicate treated
locks acquired at different times in one function as a simultaneous lockset;
sequential acquisitions were a repeatable systemic false-positive pattern.
The retained predicate emits no finding.

**Source sentence:** "When a path acquires multiple locks, acquire them in a consistent global order and document that order in a comment. Two goroutines that each hold one lock and reach for the other in opposite order deadlock under load."

**Why checkable (narrowed):** Proving *global* order consistency across the whole call graph is a whole-program lock-order-graph analysis — genuinely out of reach for a single `go/analysis` pass without unacceptable engineering cost, and exactly the kind of fragile heuristic the rigor requirement warns against. The rule is narrowed to what a single-function pass **can** honestly claim: "does this function, which acquires ≥2 distinct locks, have a comment documenting the order at the first acquisition." This is a reminder check, not a deadlock proof, and is documented as such.

**Node type(s):** `*ast.FuncDecl`/`*ast.FuncLit` body.

**Predicate:**
```go
func undocumentedMultiLock(cmap ast.CommentMap, body *ast.BlockStmt) (firstLock ast.Stmt, recvs []string) {
    seen := map[string]bool{}
    ast.Inspect(body, func(n ast.Node) bool {
        stmt, ok := n.(ast.Stmt)
        if !ok {
            return true
        }
        recv, _ := lockCallReceiver(stmt)
        if recv == "" || seen[recv] {
            return true
        }
        seen[recv] = true
        recvs = append(recvs, recv)
        if firstLock == nil {
            firstLock = stmt
        }
        return true
    })
    if len(recvs) < 2 {
        return nil, nil
    }
    if len(cmap[firstLock]) > 0 {
        return nil, nil // documented — compliant
    }
    return firstLock, recvs
}
```

**Exclusions:** only same-function, syntactically-adjacent-in-body-order lock acquisitions are considered; this predicate makes no claim about a *different* function acquiring the same locks in reverse order — that is exactly the scope this narrowing excludes, stated explicitly rather than faked with a whole-program heuristic.

**Finding.Message:** `"function at %s acquires %d distinct locks (%s) with no comment documenting acquisition order — mismatched order across call sites deadlocks under load"`

**Finding.Severity:** `SeverityWarning` — a reminder pointing at real deadlock risk, not a confirmed defect (confirming it requires the whole-program analysis this predicate deliberately does not attempt).

---

<a id="concurrency-18"></a>
### concurrency-18 — `time.After` in a `select` loop, and a local `Ticker` never `Stop`ped

**Source sentence:** "Stop every `time.Ticker` and `time.Timer`. Do not call `time.After` in a select loop: it allocates a new timer each iteration that the runtime cannot collect until the duration fires. Reuse one `time.Timer` with `Reset`, or use a `time.Ticker` you `Stop`."

**Why checkable (narrowed):** Two syntactic signatures share one rule ID: (a) a `time.After(...)` call used as a `select` receive inside an enclosing loop, and (b) a local identifier assigned from `time.NewTicker` that is never passed to `.Stop()` in its owning function. `time.NewTimer` and tickers assigned into fields were removed after calibration: a timer may be consumed normally, while field-owned tickers are commonly stopped by another lifecycle method.

**Node type(s) — predicate (a):** `*ast.SelectStmt` inside `*ast.ForStmt`/`*ast.RangeStmt`, `*ast.CommClause`.
```go
func selectTimeAfterInLoop(pass *analysis.Pass, sel *ast.SelectStmt, inLoop bool) []*ast.CallExpr {
    if !inLoop {
        return nil
    }
    var bad []*ast.CallExpr
    for _, c := range sel.Body.List {
        cc := c.(*ast.CommClause)
        call := recvCallFromComm(cc.Comm) // digs ExprStmt/AssignStmt -> UnaryExpr(ARROW) -> CallExpr
        if call != nil && astutil.IsPkgFunc(pass, call, "time", "After") {
            bad = append(bad, call)
        }
    }
    return bad
}
```
`inLoop` is computed by the pass tracking an ancestor stack (via `insp.WithStack` or a manual parent map) and testing for the nearest enclosing `*ast.ForStmt`/`*ast.RangeStmt` without crossing a `*ast.FuncLit` boundary.

**Node type(s) — predicate (b):** `*ast.AssignStmt` with a single identifier on the left, `*ast.CallExpr` (`time.NewTicker`).
```go
func tickerOrTimerNeverStopped(pass *analysis.Pass, assign *ast.AssignStmt, fnBody *ast.BlockStmt) bool {
    if len(assign.Rhs) != 1 {
        return false
    }
    call, ok := assign.Rhs[0].(*ast.CallExpr)
    if !ok {
        return false
    }
    if !astutil.IsPkgFunc(pass, call, "time", "NewTicker") {
        return false
    }
    if len(assign.Lhs) != 1 {
        return false
    }
    if _, ok := assign.Lhs[0].(*ast.Ident); !ok {
        return false
    }
    varName := types.ExprString(assign.Lhs[0])
    stopped := false
    ast.Inspect(fnBody, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }
        sel, ok := call.Fun.(*ast.SelectorExpr)
        if ok && sel.Sel.Name == "Stop" && types.ExprString(sel.X) == varName {
            stopped = true
        }
        return true
    })
    return !stopped
}
```

**Exclusions:** both predicates skip `_test.go`. Predicate (a) only fires when the `select` is lexically inside a loop in the same function. Predicate (b) excludes `time.NewTimer`, selector/field assignments, and any local ticker stopped in the declaring function. Cross-method lifecycle ownership is deliberately out of scope.

**Finding.Message:**
- (a): `"select at %s calls time.After(...) inside a loop — each iteration allocates a timer the runtime can't collect until it fires; reuse a time.Timer with Reset or a time.Ticker you Stop"`
- (b): `"%s created via time.%s at %s but never Stopped"`

**Finding.Severity:** `SeverityWarning` for both — resource leaks proven over sustained load in the source's own Production Note, not an instant crash; same tier as concurrency-03's buffer-size rule (same failure shape: gradual memory growth).

---

<a id="concurrency-19"></a>
### concurrency-19 — loop variable captured by reference in a go/defer closure (pre-1.22 targets only)

**Source sentence (pass-added — not one of concurrency.md's 18 numbered rules; quoted from this
remediation's own dispatching rationale instead, disclosed per the file header):** "each goroutine
can observe the final loop value instead of its own iteration's value; on repos where the language
doesn't yet protect against it, this is a correctness bug that ships silently since the race is
timing-dependent and tests are unlikely to catch it."

**Why checkable:** The capture check itself is exact, not a heuristic: whether an identifier inside
the closure denotes the *same* `types.Object` as the loop's own iteration variable
(`pass.TypesInfo.Uses[id] == pass.TypesInfo.Defs[loopVarIdent]`) — a shadowing parameter of the same
name (`func(i int){...}(i)`) is a distinct object and is never confused with the outer one, so this
is identity, not name-matching. The one semantic precondition — is the *target's* declared Go
version below 1.22 — is itself resolvable via a public API, not a guess (see "Version gating"
below); the predicate must never fire above that version because the language itself fixed the bug
in Go 1.22 (each iteration gets a fresh variable).

**Version gating:** `pass.Pkg` is a `*go/types.Package`, already populated by the `NeedTypes` load
bit `contracts.md`'s `audit.Run` sets for every domain — no new `Need*` bit, no orchestration
change. `(*types.Package).GoVersion() string` is public since Go 1.21 and returns the effective
language version the type-checker used for that package (the `go/packages` loader derives it from
the module's `go.mod` `go` directive, or the toolchain default when absent) as `"go1.21"`,
`"go1.22"`, etc., or `""` if unresolved. The stdlib `go/version` package (public since Go 1.22 —
this module is `go 1.25`, so it is always importable here) does the comparison correctly, including
prereleases and patch suffixes: `version.Compare(pass.Pkg.GoVersion(), "go1.22") < 0`. Per that
function's documented contract, an unresolved (`""`) version compares less than any valid version —
so "version unknown" is automatically treated as "assume pre-1.22, evaluate the predicate": a
missed goroutine-capture race is worse than a dismissible false positive on a version-ambiguous
package. This is package-level granularity, not file-level — `go/types.Info.FileVersions
map[*ast.File]string` (also public, added in 1.22 specifically for `//go:build go1.22`-gated files
inside an older-`go.mod` package) would resolve that finer skew, but is deliberately not used here;
documented narrowing, not silently dropped. The real
`golang.org/x/tools/go/analysis/passes/loopclosure` analyzer — the upstream analyzer for this exact
bug class — solves this identical version-gating problem via `golang.org/x/tools/internal/versions`,
which is unexported to the x/tools module tree and cannot be imported from this project's own
`internal/analysis/concurrency` package; `pass.Pkg.GoVersion()` plus the stdlib `go/version` package
is the clean, public replacement for that internal helper, not an invented API. The gate is checked
once per package before any AST walk for this rule (a per-package short-circuit, not a global
no-op): `RegisterRule("concurrency-19", ...)` still runs unconditionally at `init()`, so a typo'd
rule ID still fails loud in `go test ./...` per this file's existing `astutil.Report` panic-on-
unregistered contract — only the walk is skipped on a `go1.22`+ target, never the registration.

**Node type(s):** `*ast.RangeStmt` / `*ast.ForStmt` (loop-defined vars only — the range clause's or
`Init`'s own `:=`), `*ast.GoStmt` / `*ast.DeferStmt` with an `*ast.FuncLit` callee, direct statements
of the loop's own `*ast.BlockStmt` body.

**Predicate:**
```go
func loopDefinedVars(pass *analysis.Pass, loop ast.Node) []types.Object {
    var idents []*ast.Ident
    switch l := loop.(type) {
    case *ast.RangeStmt:
        if l.Tok != token.DEFINE {
            return nil // `for k, v = range x` reuses outer vars — not this bug
        }
        if k, ok := l.Key.(*ast.Ident); ok && k.Name != "_" {
            idents = append(idents, k)
        }
        if v, ok := l.Value.(*ast.Ident); ok && v.Name != "_" {
            idents = append(idents, v)
        }
    case *ast.ForStmt:
        assign, ok := l.Init.(*ast.AssignStmt)
        if !ok || assign.Tok != token.DEFINE {
            return nil
        }
        for _, lhs := range assign.Lhs {
            if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
                idents = append(idents, id)
            }
        }
    }
    var objs []types.Object
    for _, id := range idents {
        if obj := pass.TypesInfo.Defs[id]; obj != nil {
            objs = append(objs, obj)
        }
    }
    return objs
}

// shadowsLoopVar reports the classic pre-1.22 idiomatic fix `x := x` where
// the right side denotes one of loopVars — the fix this rule must not flag.
func shadowsLoopVar(pass *analysis.Pass, stmt ast.Stmt, loopVars []types.Object) bool {
    assign, ok := stmt.(*ast.AssignStmt)
    if !ok || assign.Tok != token.DEFINE {
        return false
    }
    for _, rhs := range assign.Rhs {
        rid, ok := rhs.(*ast.Ident)
        if !ok {
            continue
        }
        obj := pass.TypesInfo.Uses[rid]
        for _, lv := range loopVars {
            if obj == lv {
                return true
            }
        }
    }
    return false
}

// capturedLoopVars reports every identifier inside lit's body that resolves,
// by types.Object identity (not name), to one of loopVars.
func capturedLoopVars(pass *analysis.Pass, lit *ast.FuncLit, loopVars []types.Object) []*ast.Ident {
    var bad []*ast.Ident
    ast.Inspect(lit.Body, func(n ast.Node) bool {
        id, ok := n.(*ast.Ident)
        if !ok {
            return true
        }
        obj := pass.TypesInfo.Uses[id]
        for _, lv := range loopVars {
            if obj == lv {
                bad = append(bad, id)
            }
        }
        return true
    })
    return bad
}

// loopVarCaptureViolations walks the direct statements of loop's body (see
// Exclusions for why only direct statements) for a go/defer whose *ast.FuncLit
// callee captures a loop-defined variable by reference, with no preceding
// shadow-fix earlier in the same block.
func loopVarCaptureViolations(pass *analysis.Pass, loopVars []types.Object, body *ast.BlockStmt) map[ast.Stmt][]*ast.Ident {
    violations := map[ast.Stmt][]*ast.Ident{}
    shadowed := false
    for _, stmt := range body.List {
        if shadowsLoopVar(pass, stmt, loopVars) {
            shadowed = true
            continue
        }
        if shadowed {
            continue
        }
        var lit *ast.FuncLit
        switch s := stmt.(type) {
        case *ast.GoStmt:
            lit, _ = s.Call.Fun.(*ast.FuncLit)
        case *ast.DeferStmt:
            lit, _ = s.Call.Fun.(*ast.FuncLit)
        }
        if lit == nil {
            continue
        }
        if bad := capturedLoopVars(pass, lit, loopVars); len(bad) > 0 {
            violations[stmt] = bad
        }
    }
    return violations
}
```
Dispatch skips this rule entirely for a package where `version.Compare(pass.Pkg.GoVersion(),
"go1.22") >= 0` — checked once, before calling `loopDefinedVars`/`loopVarCaptureViolations` at all.

**Exclusions:**
- `go f(i)` or `go func(i int){...}(i)` (loop var passed as an explicit call argument) — the first
  form has no `*ast.FuncLit` callee at all; in the second, the literal's own parameter `i` is a
  distinct `types.Object` from the outer loop variable, so every reference inside the literal's
  body fails the identity check in `capturedLoopVars` — this is why the check is identity-based, not
  name-based.
- `i := i` before the go/defer, same block — `shadowsLoopVar` detects it and every subsequent
  go/defer in that block is treated as shadowed, matching the real idiom of shadowing once near the
  top of a loop body and reusing it for every goroutine spawned after.
- `*ast.RangeStmt` using `=` instead of `:=` (reuses a pre-existing outer variable — no per-iteration
  variable exists at all) — `loopDefinedVars` returns nil, so no capture can ever match.
- A go/defer nested inside an `if`/`switch`/`select` within the loop body (not a direct statement of
  the loop's own `*ast.BlockStmt`) is out of scope: proving shadow-order across arbitrary nested
  control flow needs a full scope-tree walk, not a same-block sibling scan. The direct-child-of-
  loop-body shape covers the overwhelming majority of real occurrences of this bug; documented
  narrowing, not a claim of full coverage — same posture as concurrency-17's narrowing.
- Any target whose `pass.Pkg.GoVersion()` compares `>= "go1.22"` (see Version gating) — zero AST
  walk for this rule on that package, not just zero findings.

**Finding.Message:** `"%s at %s is captured by reference in a go/defer closure inside a loop — this target's go.mod declares a pre-1.22 Go version, so every iteration's closure can observe the loop's final value instead of its own; pass it as an explicit argument or shadow it with %s := %s before the closure"`

**Finding.Severity:** `SeverityError` — a silent data race/wrong-value bug: each goroutine can
observe the final loop value instead of its own iteration's, and because the failure is timing-
dependent, tests are unlikely to catch it — same tier as concurrency-01/06/08's silent,
hard-to-detect production defects, not a style nit.

---

<a id="concurrency-20"></a>
### concurrency-20 — `defer` directly inside a loop, unscoped

**v0.1.0 status:** Disabled after external calibration. Real projects use this
shape for bounded retries, deliberate function-lifetime lock ownership, and
other bounded cleanup. The AST predicate cannot prove harmful accumulation,
so the retained predicate emits no finding.

**Source sentence (pass-added — not one of concurrency.md's 18 numbered rules; quoted from this
remediation's own dispatching rationale, disclosed per the file header):** "a resource-accumulation
trap (unclosed file handles, unreleased locks, growing defer stack) that becomes a real leak once
the loop bound grows unbounded or long-running" — "not always wrong (a defer in a loop with small
bounded iterations is often fine)."

**Why checkable:** A `*ast.DeferStmt`'s nearest enclosing loop is a parent-chain question. A small
bounded `range` exclusion is also decidable for composite literals, integer constants, and
non-reassigned local variables initialized from either form, so the rule remains checkable at
every supported Go version, unlike concurrency-19.

**Node type(s):** `*ast.DeferStmt`, walked via an ancestor stack (`inspector.Inspector.WithStack` or
an equivalent manual parent map — the same mechanism concurrency-18's `inLoop` already uses above).

**Predicate:**
```go
// deferEnclosingLoop walks stack (outermost-first, ending in the *ast.DeferStmt
// itself, per inspector.WithStack) for the nearest enclosing *ast.ForStmt/
// *ast.RangeStmt. It stops at the first *ast.FuncLit/*ast.FuncDecl boundary: a
// defer inside a function literal is always scoped to that literal's own
// call, regardless of whether the literal itself sits in a loop — that's the
// correct per-iteration-scoping fix, not a violation.
func deferEnclosingLoop(stack []ast.Node) (loop ast.Node, found bool) {
    for i := len(stack) - 2; i >= 0; i-- {
        switch n := stack[i].(type) {
        case *ast.ForStmt:
            return n, true
        case *ast.RangeStmt:
            return n, true
        case *ast.FuncLit:
            return nil, false
        case *ast.FuncDecl:
            return nil, false
        }
    }
    return nil, false
}
```

**Exclusions:** a defer whose ancestor walk hits a `*ast.FuncLit` boundary before reaching a
`*ast.ForStmt`/`*ast.RangeStmt` is correctly scoped and must not fire — this is the
`func(){ defer f.Close() }()` immediately-invoked-closure fix. Stopping at *any* `*ast.FuncLit`
boundary (not just a detected immediate invocation) is deliberate and still correct in the general
case: a defer inside a closure that is stored or passed rather than invoked inline is still scoped
to that closure's own eventual call, never to the outer loop, so this exclusion is not IIFE-specific
even though the fixture demonstrates the IIFE form.

A `range` loop that is provably bounded to at most eight iterations is also excluded. The bound is
provable when the range expression is a composite literal with at most eight elements, an integer
constant from zero through eight, or a local variable initialized from one of those expressions
and never reassigned. This calibration guard suppresses harmless small batches without guessing at
calls or general loop conditions.

**Finding.Message:** `"defer at %s is directly inside a %s loop with no enclosing function-literal scope — it accumulates until the enclosing function returns instead of running per iteration; wrap the loop body in an immediately-invoked closure (func(){ defer ... }()) or move the deferred call outside the loop"`

**Finding.Severity:** `SeverityWarning` — not always wrong: a defer in a loop with a small, bounded
iteration count is often fine, so the predicate suppresses bounds it can prove are at most eight.
It becomes a real leak
(unclosed file handles, unreleased locks, growing defer stack) once the loop's bound grows unbounded
or long-running, so it is flagged as a resource-accumulation trap to review, not asserted as a
proven defect — same tier as concurrency-03's buffer-size rule (same failure shape: gradual resource
growth, not an instant crash).

---

## 3. Fixture file spec

The fixture material below is retained for future redesign; it is not the v0.1.0 active inventory.
Directory: `internal/tools/testdata/fixtures/audit-concurrency/`. One Go package per documented
rule (`rule01/` .. `rule10/`, `rule12/`, `rule14/`, `rule15/`, `rule17/` .. `rule20/` — 17
directories; no `rule11`/`rule13`/`rule16` since those rules are excluded and never registered,
so no fixture exists for an unregistered rule ID), per `contracts.md`'s Testdata fixture
layout. `rule19/` is the one deliberate exception to "no fixture declares its own `go.mod`": it
needs a target with a real, pre-1.22 `go` directive to exercise a version-gated rule, so it
carries its own nested `go.mod` (see its fixture listing below) — a documented one-off, not a
new convention. Each rule package contains exactly one `violation.go` (the true-positive case) and one
`compliant.go` (the near-miss) as separate files — never a single file mixing both, so a stray
finding can never be misattributed to a sibling rule. `// VIOLATION: concurrency-NN` /
`// COMPLIANT: concurrency-NN` markers sit directly above the relevant line. Where
`violation.go` and `compliant.go` share a same-package symbol (e.g. rule07's `Counter`, rule09's
`startupDone`, rule14's `bufPool`, rule17's `Pair`), it's declared once, in `violation.go`;
`compliant.go` uses it directly via same-package visibility — no import needed, and no third
support file invented.

**`rule01/violation.go`** (concurrency-01)
```go
package rule01

type Handler struct{}

func (h *Handler) Serve(job string) {
	// VIOLATION: concurrency-01
	go h.process(job)
}

func (h *Handler) process(job string) {}
```

**`rule01/compliant.go`**
```go
package rule01

import "context"

type OwnedHandler struct{}

func (h *OwnedHandler) ServeOwned(ctx context.Context, job string) {
	// COMPLIANT: concurrency-01
	go h.process2(ctx, job)
}

func (h *OwnedHandler) process2(ctx context.Context, job string) {}
```

**`rule02/violation.go`** (concurrency-02)
```go
package rule02

import "context"

// VIOLATION: concurrency-02
type LeakyWorker struct{}

func (w *LeakyWorker) Run(ctx context.Context) {
	go w.loop(ctx)
}

func (w *LeakyWorker) loop(ctx context.Context) {}
```

**`rule02/compliant.go`**
```go
package rule02

import "context"

// COMPLIANT: concurrency-02
type OwnedWorker struct{}

func (w *OwnedWorker) Run(ctx context.Context) {
	go w.loop(ctx)
}

func (w *OwnedWorker) loop(ctx context.Context) {}

func (w *OwnedWorker) Stop() {}
```

**`rule03/violation.go`** (concurrency-03)
```go
package rule03

func MakeUnjustifiedQueue() {
	// VIOLATION: concurrency-03
	_ = make(chan int, 100)
}
```

**`rule03/compliant.go`**
```go
package rule03

func MakeJustifiedQueue() {
	// buffered to 1 so a single slow consumer never blocks the fast producer
	// COMPLIANT: concurrency-03
	_ = make(chan int, 1)
}
```

**`rule04/violation.go`** (concurrency-04)
```go
package rule04

// VIOLATION: concurrency-04
func Undirected(c chan int) {}
```

**`rule04/compliant.go`**
```go
package rule04

// COMPLIANT: concurrency-04
func Directed(in <-chan int, out chan<- int) {}
```

**`rule05/violation.go`** (concurrency-05)
```go
package rule05

// VIOLATION: concurrency-05
func ProcessAsync(item string) {
	go func() { _ = item }()
}
```

**`rule05/compliant.go`**
```go
package rule05

// COMPLIANT: concurrency-05
func ProcessAsyncSignal(item string) <-chan struct{} {
	done := make(chan struct{}, 1)
	go func() {
		_ = item
		done <- struct{}{}
	}()
	return done
}
```

**`rule06/violation.go`** (concurrency-06)
```go
package rule06

import "context"

func FetchAll(ctx context.Context, urls []string) {
	for _, u := range urls {
		u := u
		// VIOLATION: concurrency-06
		go func() {
			_, _ = fetch(context.Background(), u)
		}()
	}
}

func fetch(ctx context.Context, url string) (string, error) { return "", nil }
```

**`rule06/compliant.go`**
```go
package rule06

import "context"

func FetchAllOK(ctx context.Context, urls []string) {
	for _, u := range urls {
		u := u
		// COMPLIANT: concurrency-06
		go func() {
			_, _ = fetch(ctx, u)
		}()
	}
}
```

**`rule07/violation.go`** (concurrency-07)
```go
package rule07

import "sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) IncBad() {
	// VIOLATION: concurrency-07
	c.mu.Lock()
	c.n++ // work happens before the defer — panic window
	defer c.mu.Unlock()
}
```

**`rule07/compliant.go`**
```go
package rule07

func (c *Counter) IncGood() {
	// COMPLIANT: concurrency-07
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}
```

**`rule08/violation.go`** (concurrency-08)
```go
package rule08

import "sync"

// VIOLATION: concurrency-08
type BadCache struct {
	sync.Mutex
	data map[string]string
}
```

**`rule08/compliant.go`**
```go
package rule08

import "sync"

// COMPLIANT: concurrency-08
type GoodCache struct {
	mu   sync.Mutex
	data map[string]string
}
```

**`rule09/violation.go`** (concurrency-09)
```go
package rule09

var startupDone = make(chan struct{}, 1)

func init() {
	// VIOLATION: concurrency-09
	go func() { startupDone <- struct{}{} }()
}
```

**`rule09/compliant.go`**
```go
package rule09

func Start() {
	// goroutine lives in an explicit Start, not init
	// COMPLIANT: concurrency-09
	go func() { startupDone <- struct{}{} }()
}
```

**`rule10/violation.go`** (concurrency-10)
```go
package rule10

import "sync"

// VIOLATION: concurrency-10
type Registry struct {
	mu    sync.Mutex
	items map[string]string
}
```

**`rule10/compliant.go`**
```go
package rule10

import "sync"

// Registry is safe for concurrent use; all access goes through mu.
// COMPLIANT: concurrency-10
type SafeRegistry struct {
	mu    sync.Mutex
	items map[string]string
}
```

**`rule12/violation.go`** (concurrency-12)
```go
package rule12

import "sync"

func RunAll(fns []func() error) error {
	// VIOLATION: concurrency-12
	var wg sync.WaitGroup
	errCh := make(chan error)
	for _, fn := range fns {
		fn := fn
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- fn()
		}()
	}
	wg.Wait()
	return <-errCh
}
```

**`rule12/compliant.go`**
```go
package rule12

import "sync"
func RunAllSingle(fn func() error) error {
	// only one goroutine — no coordination problem
	// COMPLIANT: concurrency-12
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- fn()
	}()
	wg.Wait()
	return <-errCh
}
```

**`rule14/violation.go`** (concurrency-14)
```go
package rule14

import (
	"bytes"
	"sync"
)

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func BorrowBad() {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.WriteString("x")
	// VIOLATION: concurrency-14
	bufPool.Put(buf)
}
```

**`rule14/compliant.go`**
```go
package rule14

import "bytes"

func BorrowGood() {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.WriteString("x")
	buf.Reset()
	// COMPLIANT: concurrency-14
	bufPool.Put(buf)
}
```

**`rule15/violation.go`** (concurrency-15)
```go
package rule15

import "sync/atomic"

type BadStats struct {
	hits   atomic.Int64
	misses atomic.Int64
}

// VIOLATION: concurrency-15
func (s *BadStats) RecordMiss() {
	s.hits.Add(0)
	s.misses.Add(1)
}
```

**`rule15/compliant.go`**
```go
package rule15

import "sync/atomic"

type GoodCounter struct {
	total atomic.Int64
}

// COMPLIANT: concurrency-15
func (c *GoodCounter) Inc() {
	c.total.Add(1)
}
```

**`rule17/violation.go`** (concurrency-17)
```go
package rule17

import "sync"

type Pair struct {
	aMu, bMu sync.Mutex
}

func (p *Pair) TransferBad() {
	// VIOLATION: concurrency-17
	p.aMu.Lock()
	defer p.aMu.Unlock()
	p.bMu.Lock()
	defer p.bMu.Unlock()
}
```

**`rule17/compliant.go`**
```go
package rule17

func (p *Pair) TransferGood() {
	// Lock order: always aMu before bMu, project-wide convention.
	// COMPLIANT: concurrency-17
	p.aMu.Lock()
	defer p.aMu.Unlock()
	p.bMu.Lock()
	defer p.bMu.Unlock()
}
```

**Illustrative combined `rule18/violation.go`** (the executable fixtures split these predicates between `rule18/` and `rule18ticker/`)
```go
package rule18

import (
	"context"
	"time"
)

func LoopBad(ctx context.Context) {
	for {
		// VIOLATION: concurrency-18
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}
	}
}

func NewUnstoppedTicker() *time.Ticker {
	// VIOLATION: concurrency-18
	t := time.NewTicker(time.Second)
	return t
}
```

**`rule18/compliant.go`**
```go
package rule18

import (
	"context"
	"time"
)

func LoopGood(ctx context.Context) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		// COMPLIANT: concurrency-18
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(5 * time.Minute)
		}
	}
}

func NewStoppedTicker() *time.Ticker {
	t := time.NewTicker(time.Second)
	// COMPLIANT: concurrency-18
	t.Stop()
	return t
}
```
The executable fixture layout keeps the predicates isolated: `rule18/` covers
`select`/`time.After`, while `rule18ticker/` covers a local never-stopped
ticker. Each package has its own compliant counterpart and yields exactly one
finding.

**`rule19/go.mod`** — the one deliberate exception noted above: this fixture needs its own
pre-1.22 `go` directive so the version gate in §2 actually evaluates the predicate.
```
module fixtures/audit-concurrency/rule19

go 1.21
```

**`rule19/violation.go`** (concurrency-19)
```go
package rule19

func SpawnAll(items []string) {
	for _, item := range items {
		// VIOLATION: concurrency-19
		go func() {
			process(item)
		}()
	}
}

func process(s string) {}
```

**`rule19/compliant.go`** — the idiomatic pre-1.22 shadow fix (`item := item`), which
`shadowsLoopVar` in §2 detects and treats as clearing every subsequent go/defer in the block.
```go
package rule19

func SpawnAllFixed(items []string) {
	for _, item := range items {
		item := item
		// COMPLIANT: concurrency-19
		go func() {
			process(item)
		}()
	}
}
```

**`rule20/violation.go`** (concurrency-20)
```go
package rule20

import "os"

func ProcessAll(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		// VIOLATION: concurrency-20
		defer f.Close()
	}
	return nil
}
```

**`rule20/compliant.go`** — the defer moved into an immediately-invoked closure, so it runs
per-iteration instead of accumulating until `ProcessAllFixed` returns. The implemented fixture also
covers direct constant bounds and a non-reassigned two-element local collection.
```go
package rule20

import "os"

func ProcessAllFixed(paths []string) error {
	for _, p := range paths {
		if err := func() error {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			// COMPLIANT: concurrency-20
			defer f.Close()
			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}
```

---

## 4. Tool file spec

### Analysis subpackage — `internal/analysis/concurrency/concurrency.go`

Pasted verbatim from `contracts.md`'s conformance block, substituting `concurrency` for
`<domain>`. The 17 registrations comprise 8 active `RegisterRule` calls and 9 calibrated-off
`RegisterDisabledRule` calls. The retained dispatch from section 2 is the per-rule elaboration of the single representative
`astutil.Report` call shown below — this is the shape, not a re-enumeration of section 2.

```go
package concurrency // never package analysis — see Naming and file layout in contracts.md

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
	// Calibrated-off rules retain metadata but emit no v0.1.0 findings.
	astutil.RegisterDisabledRule("concurrency-01", "fire_and_forget_goroutine", finding.SeverityError, "external calibration found joined goroutines coordinated through result channels")
	// one RegisterRule call per active rule in this domain (concurrency-04, -05,
	// -08, -09, -10, -12, -18, -19 — matching section 1's
	// active inventory), plus one RegisterDisabledRule call per calibrated-off rule.
	// Excluded rules are never registered. astutil.Report suppresses calibrated-off rules and panics at
	// analyzer-init/test time on an unregistered
	// rule ID, by design: a typo'd rule ID must fail loud in `go test ./...`, never ship
	// silently with an empty Severity.
}

var Analyzer = &analysis.Analyzer{
	Name:     "concurrency",
	Doc:      "Audits selected goroutine and channel conventions.",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{ /* node types section 2 cares about, per rule */ }, func(n ast.Node) {
		// per-rule detection predicate from section 2; on match:
		astutil.Report(pass, n.Pos(), "concurrency-NN", "<message template %s>", arg)
	})
	return nil, nil // findings are collected via pass.Report -> Action.Diagnostics, not the return value
}
```

`Analyzer` is the **only** exported symbol from this subpackage. This domain does not declare
`RunConcurrency`, `ConcurrencyAnalyzer`, or any other entry point, driver, or `Run<Domain>`/
`run<Domain>Pass`/`mustRun<Domain>` wrapper — the single `audit.Run` call in the tool handler below
is the only call site, per `contracts.md`.

### Orchestration

`internal/audit.Run(ctx, ws, pattern, analyzers)` is the one project-wide entry point — reproduced,
unchanged, from `contracts.md`'s Orchestration section. The concurrency domain contributes
`concurrency.Analyzer` to that call; it implements no `packages.Load`, no `checker.Analyze`, and no
dedup logic of its own. `audit.Run` internally handles `packages.Load` with `Tests: true` and the
full `Need*` mode bits including `NeedTypesSizes` (required project-wide — its absence panics
performance-02); this domain does not re-derive a narrower load mode.

### Tool file — `internal/tools/go_audit_concurrency.go`

`CacheTTL: 0` (same as `go_audit_errors`/`go_audit_performance` — analysis output goes stale the
instant source changes).

```go
package tools

import (
	"context"
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/concurrency"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditConcurrencyInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default is all severities"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditConcurrencyOutput struct {
	Result finding.AuditResult `json:"result"`
}

func AuditConcurrencyHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditConcurrencyInput) (*mcp.CallToolResult, AuditConcurrencyOutput, error) {
	if err := normalizeAuditConcurrencyInput(&in); err != nil {
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("validating input: %w", err)
	}

	ws, err := resolveInWorkspace(in.Package)
	if err != nil {
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{concurrency.Analyzer})
	if err != nil {
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("running concurrency audit: %w", err)
	}
	result = finding.Filter(result, in.MinSeverity, in.MaxFindings)

	return nil, AuditConcurrencyOutput{Result: result}, nil
}

func RegisterAuditConcurrency(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_audit_concurrency",
		Description: "Runs the active concurrency-04, -05, -08, -09, -10, -12, -18, -19 go/analysis passes over a package and returns findings.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, AuditConcurrencyHandler)
}
func normalizeAuditConcurrencyInput(in *AuditConcurrencyInput) error {
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
can emit is one of the 8 active IDs in section 1: calibrated-off IDs
`concurrency-01`, `concurrency-02`, `concurrency-03`, `concurrency-06`, `concurrency-07`,
`concurrency-14`, `concurrency-15`, `concurrency-17`, and `concurrency-20` are registered as
disabled metadata and suppressed by `astutil.Report`. Excluded IDs `concurrency-11`,
`concurrency-13`, and `concurrency-16` are never registered or computed.

---

## 5. Verification

The v0.1.0 suite runs positive and near-miss fixtures for the 8 active rules,
asserts the 9 calibrated-off IDs separately, and keeps a standing exclusion
guard. The executable checks live in
`internal/analysis/concurrency/integration_test.go`; isolated packages live
under `internal/analysis/concurrency/testdata/rule<NN>/` and are loaded through
`audit.Run`. Additional `_test.go`, test-support-path, pure-wrapper, field-owned
ticker, and one-shot timer fixtures lock in the corpus-driven exclusions. The
older code sketches below illustrate the intended assertion shape; the paths
and helpers in the executable test are canonical.

Future-redesign test shape for calibrated-off `concurrency-01` (not run in v0.1.0):

```go
func TestAuditConcurrency_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule01")

	require.Len(t, findings, 1, "expected exactly one concurrency-01 finding")
	f := findings[0]
	assert.Equal(t, "concurrency-01", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-concurrency/rule01/violation.go", f.Location.File)
	assert.Equal(t, 7, f.Location.Line) // the `go h.process(job)` line
}

func TestAuditConcurrency_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule01")
	for _, f := range findings {
		// scoped to the compliant fixture file specifically — the sibling
		// violation.go finding in this same fixture load legitimately does
		// carry "concurrency-01" (asserted above), so an unscoped "no
		// finding anywhere carries this rule ID" assertion cannot hold
		// alongside it; resolved per phase-4a-naming.md's precedent for the
		// identical CONTRACTS-level tension in the naming domain.
		if f.Location.File == "fixtures/audit-concurrency/rule01/compliant.go" {
			assert.NotEqual(t, "concurrency-01", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
```

The active single-predicate fixtures apply the same positive/near-miss expectations, each asserting
its one `Finding` at the documented violating
line in `fixtures/audit-concurrency/rule<NN>/violation.go`:

| Rule | Violation line | Rule | Violation line | Rule | Violation line |
|---|---|---|---|---|---|
| concurrency-04 | 4 | concurrency-08 | 7 | concurrency-12 | 5 |
| concurrency-05 | 4 | concurrency-09 | 5 | concurrency-10 | 6 |

`concurrency-18` is exercised by two fixture packages under one rule ID:
`rule18` for `time.After` inside a `select` loop and `rule18ticker` for a local
`time.NewTicker` never `Stop`ped. The following is an illustrative combined
assertion; `integration_test.go` iterates the two directories separately:

```go
func TestAuditConcurrency_Rule18(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule18")
	require.Len(t, findings, 2) // one time.After-in-select, one un-Stopped ticker
	for _, f := range findings {
		assert.Equal(t, "concurrency-18", f.Rule)
		assert.Equal(t, "fixtures/audit-concurrency/rule18/violation.go", f.Location.File)
	}
	lines := []int{findings[0].Location.Line, findings[1].Location.Line}
	assert.Contains(t, lines, 11) // LoopBad's `select` statement
	assert.Contains(t, lines, 21) // NewUnstoppedTicker's `t := time.NewTicker(...)` line
}

func TestAuditConcurrency_Rule18_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule18")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-concurrency/rule18/compliant.go" {
			assert.NotEqual(t, "concurrency-18", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
```

`concurrency-19` is the one rule whose fixture package carries its own `go.mod` (see §3), so its
version gate is exercised by construction — `astutil.RunFixture` loads `rule19/` as its own module
with the pinned pre-1.22 `go` directive, never falling back to this spec tree's own `go 1.25`:

```go
func TestAuditConcurrency_Rule19(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule19")

	require.Len(t, findings, 1, "expected exactly one concurrency-19 finding")
	f := findings[0]
	assert.Equal(t, "concurrency-19", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-concurrency/rule19/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line) // the `go func() {` line capturing `item` by reference
}

func TestAuditConcurrency_Rule19_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule19")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-concurrency/rule19/compliant.go" {
			assert.NotEqual(t, "concurrency-19", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditConcurrency_Rule20(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule20")

	require.Len(t, findings, 1, "expected exactly one concurrency-20 finding")
	f := findings[0]
	assert.Equal(t, "concurrency-20", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-concurrency/rule20/violation.go", f.Location.File)
	assert.Equal(t, 12, f.Location.Line) // the `defer f.Close()` line, directly inside the loop body
}

func TestAuditConcurrency_Rule20_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency/rule20")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-concurrency/rule20/compliant.go" {
			assert.NotEqual(t, "concurrency-20", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
```

Domain-wide rule count:

```go
func TestAuditConcurrency_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("concurrency"), 8) // catches an active rule silently dropped or added
}
```

Standing regression guard, kept beyond the strict per-rule template (precedented by
`phase-4a-errors.md`'s own `TestAuditErrors_ExcludedRulesNotEmitted`) — `concurrency-11`,
`concurrency-13`, and `concurrency-16` are excluded and never registered, so running the analyzer
over the whole fixture tree must never surface any of the three, from any rule's fixtures:

```go
func TestAuditConcurrency_ExcludedRulesNotEmitted(t *testing.T) {
	findings := astutil.RunFixture(t, concurrency.Analyzer, "audit-concurrency")
	assert.Empty(t, astutil.FindingsForRule(findings, "concurrency-11"), "concurrency-11 is excluded — no predicate should ever emit it")
	assert.Empty(t, astutil.FindingsForRule(findings, "concurrency-13"), "concurrency-13 is excluded — no predicate should ever emit it")
	assert.Empty(t, astutil.FindingsForRule(findings, "concurrency-16"), "concurrency-16 is excluded — no predicate should ever emit it")
}
```
