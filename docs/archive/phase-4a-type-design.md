# Phase 4 — typedesign audit (`internal/analysis/typedesign/`)

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

The original type-design corpus is reproduced here and converted using
`contracts.md`'s 6-step AST-pattern recipe.
`Finding.Rule` values are `typedesign-<NN>` (e.g. `typedesign-01`) — this is
contracts.md's universal `<domain>-<NN>` format (see "Canonical shared types"
and the fixture-layout section there), not a per-file exception. This file
already conforms; no divergent convention applies here.

## Section 1 — Rule table

| Rule ID | Name | Source | Checkable core |
|---|---|---|---|
| `typedesign-01` | empty-interface-literal | Rule 1 | `*ast.InterfaceType` with 0 methods written as literal `interface{}` |
| `typedesign-02` | producer-side-interface | Rule 2 | interface + its implementer(s) declared in the same package |
| `typedesign-03` | premature-interface | Rule 4 | interface with ≤1 implementer in package scope |
| `typedesign-04` | interface-return-type | Rule 5 | func result type is a non-`error` named interface |
| `typedesign-05` | pointer-to-interface | Rule 6 | `*ast.StarExpr` whose operand type is `*types.Interface` |
| `typedesign-06` | missing-compile-time-assertion | Rule 7 | type satisfies a package-referenced interface, no `var _ I = (*T)(nil)` |
| `typedesign-07` | embedded-field-in-exported-struct | Rule 8 | anonymous field inside an exported `*ast.StructType`, excluding `sync.*` embeds (that's `concurrency-08`) |
| `typedesign-08` | zero-value-enum-not-reserved | Rule 11 | `iota`-const block whose index-0 name isn't an "unset" name |
| `typedesign-09` | enum-missing-stringer | Rule 12 | named int-kind type with ≥2 consts, no `String() string` method |
| `typedesign-10` | inconsistent-json-tags | Rule 13 | struct has ≥1 `json:` tag, sibling exported field has none |
| `typedesign-11` | primitive-type-alias | Rule 16 | `type X = Y` alias where `Y` underlying is a basic primitive kind |
| `typedesign-12` | mixed-receiver-types | Rule 17 | same named type has both pointer- and value-receiver methods |
| `typedesign-13` | value-receiver-on-unsafe-copy-type | Rule 17 | type embeds `sync.Mutex`/`sync.RWMutex`, has a value-receiver method |
| `typedesign-14` | exported-getter-returns-mutable-internal | Rule 18 | exported method body is bare `return recv.field` for slice/map field |
| `typedesign-15` | trivial-constructor-wrapper | Rule 15 | zero-arg `NewX()` whose body is only `return &T{}` / `return T{}` |
| `typedesign-16` | single-call-site-generic | Rule 20 / Trap 7 | unexported generic func with exactly 1 in-package call site |
| `typedesign-17` | nil-typed-pointer-as-interface | Trap 6 / Production Note | `var p *T` (no reassignment) returned into an interface-typed result |

17 checkable rules. 8 excluded (listed at the end of Section 2) — same source file, judgment-call or out-of-scope, not converted into fragile heuristics.

## Section 2 — Per-rule detail

### Shared helpers

Every rule below calls `astutil.Report(pass, pos, rule, tmpl, args...)` to emit a `finding.Finding` — never a bare `report(pos)` (that name was a placeholder in an earlier draft of this file, not a real symbol; it never took a message, which is why every call site below now shows the actual `tmpl`/`args` the message needs). Severity is resolved internally via `astutil.RuleSeverity(rule)`, so no call below passes a severity argument. Every rule ID used in a `Report` call is registered once, in this domain's `init()`, via `astutil.RegisterRule("typedesign-NN", "<name>", finding.Severity<X>)` — never inline at the call site (per contracts.md's rule→AST-pattern recipe, step 5).

`astutil.IsPkgFunc`/`astutil.IsMethodOn` exist for call-shape detection against a *known* external `pkgPath`+name (e.g. "is this call `bytes.NewBuffer(...)`"). No rule in this domain needs that shape: `typedesign-06`'s call-site scan matches an argument's static type against a locally-observed interface parameter (a `go/types` fact, not a known-callee match), and `typedesign-16`'s call-site count matches on the generic function's own `types.Object` identity within the same package (via `pass.TypesInfo.Uses`/`Defs`), not against a known package function. Both are documented as such in this file; neither helper is called by name below — noted here rather than forcing an artificial call site.

Locally-declared helpers, genuinely typedesign-specific (not promoted to `astutil` — no other domain's rules reference them):

| Helper | Signature | Used by |
|---|---|---|
| `isSyncType` | `func(t types.Type) bool` | typedesign-07 |
| `usesIota` | `func(vs *ast.ValueSpec) bool` | typedesign-08 |
| `unsetNameRe` | `*regexp.Regexp` (package-level var, `(?i)unspecified\|unknown\|none\|invalid\|undefined`) | typedesign-08 |
| `matchesAssertedType` | `func(v ast.Expr, t *types.Named) bool` | typedesign-06 |
| `recvBaseName` | `func(expr ast.Expr) (name string, isPtr bool)` | typedesign-12, typedesign-13 |
| `kindOf` | `func(isPtr bool) string` (returns `"ptr"`/`"val"`) | typedesign-12 |
| `isMutexOrRWMutex` | `func(t types.Type) bool` | typedesign-13 |
| `unwrapAddr` | `func(e ast.Expr) ast.Expr` | typedesign-15 |
| `funcResultTypeExpr` | `func(fd *ast.FuncDecl, i int) ast.Expr` | typedesign-17 |
| `declaredNilNoReassign` | `func(body *ast.BlockStmt, id *ast.Ident) bool` | typedesign-17 |

### typedesign-01 — empty-interface-literal

- **Quote**: "Use `any` instead of `interface{}`... `any` is the correct spelling (Go 1.18+); `interface{}` signals outdated code."
- **Checkable because**: `any` parses as `*ast.Ident`, never as `*ast.InterfaceType` — so any `*ast.InterfaceType` node with an empty method list appearing at a type-literal position is unambiguously the old spelling, a pure syntactic fact.
- **Predicate** — node: `*ast.InterfaceType`.
  ```go
  insp.Preorder([]ast.Node{(*ast.InterfaceType)(nil)}, func(n ast.Node) {
      it := n.(*ast.InterfaceType)
      if len(it.Methods.List) == 0 {
          // literal interface{} — any parses as *ast.Ident, never reaches here
          astutil.Report(pass, it.Pos(), "typedesign-01", "empty interface literal interface{} in %s, use any", pass.Fset.Position(it.Pos()).String())
      }
  })
  ```
  Exclusion: none needed — the AST shape alone disambiguates `any` vs `interface{}`.
- **Message**: `"empty interface literal interface{} in %s, use any"` (file:line)
- **Severity**: `SeverityInfo` — zero behavioral difference (`any` is a builtin alias), pure spelling/readability.

### typedesign-02 — producer-side-interface

- **Quote**: "Define interfaces where they are USED, not where they are implemented... Producing-side interfaces create import cycles and god-interfaces."
- **Checkable because**: whether an interface's implementer(s) live in the *same* package as the interface declaration is a `go/types.Implements` fact, not an opinion about "should."
- **Predicate** — nodes: `*ast.TypeSpec` (interface decl) + package-scope named types.
  ```go
  ifaceObj := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
  iface := ifaceObj.Type().Underlying().(*types.Interface)
  for _, name := range pass.Pkg.Scope().Names() {
      if tn, ok := pass.Pkg.Scope().Lookup(name).(*types.TypeName); ok {
          if types.Implements(tn.Type(), iface) || types.Implements(types.NewPointer(tn.Type()), iface) {
              // implementer found in same package
              astutil.Report(pass, typeSpec.Pos(), "typedesign-02", "interface %s declared alongside its own implementation in package %s, define it in the consumer package instead", typeSpec.Name.Name, pass.Pkg.Name())
          }
      }
  }
  ```
  Exclusion: skip implementers whose only defining file is `_test.go` (mock-for-test-in-same-package is the sanctioned exception per go-testing.md "mocks: interface in the consumer package" combined with "mocks: in the test file"). Skip interfaces with 0 methods (already `typedesign-01`'s territory).
  Limitation (document, don't hide): `go/analysis` passes are per-package by default — this only proves "producer == implementer," not "no consumer elsewhere ever needed it separately." Acceptable false-negative direction (under-reports, never over-reports a true consumer-side interface).
- **Message**: `"interface %s declared alongside its own implementation in package %s, define it in the consumer package instead"`
- **Severity**: `SeverityWarning` — design/import-cycle risk, not an active bug.

### typedesign-03 — premature-interface

- **Quote**: "Do not define an interface until a second implementation exists or a test requires one. Concrete types first."
- **Checkable because**: implementer count is a `types.Implements` scan (same mechanism as 02), and "0 or 1" is an objective threshold stated by the rule itself, not an invented one.
- **Predicate**: reuse the implementer-scan from `typedesign-02`, but count total implementers (any package reachable from `pass.Pkg.Imports()` plus `pass.Pkg` itself — single-package pass practically limits this to same-package + directly-imported packages' exported types).
  ```go
  count := 0
  for _, tn := range candidateNamedTypes { // same-package + imported-package exported types
      if types.Implements(tn.Type(), iface) || types.Implements(types.NewPointer(tn.Type()), iface) {
          count++
      }
  }
  if count <= 1 {
      astutil.Report(pass, typeSpec.Pos(), "typedesign-03", "interface %s has %d implementation(s) in this package, premature abstraction below the two-implementation threshold", typeSpec.Name.Name, count)
  }
  ```
  Exclusion: skip if a `_test.go` file in the package declares a struct satisfying the interface with a name matching `*Mock*`/`*Fake*`/`*Stub*` — that is the "a test requires one" exception the rule itself carves out.
- **Message**: `"interface %s has %d implementation(s) in this package, premature abstraction below the two-implementation threshold"`
- **Severity**: `SeverityWarning`.

### typedesign-04 — interface-return-type

- **Quote**: "Accept interfaces, return concrete types."
- **Checkable because**: a function's declared result type is either a named interface or not — resolvable via `pass.TypesInfo.TypeOf` on the result field's type expression, no judgment required once `error` and the constructor-idiom exclusion are carved out.
- **Predicate** — node: `*ast.FuncDecl` / `*ast.FuncType.Results`.
  ```go
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if fd.Type.Results == nil {
          return
      }
      for _, res := range fd.Type.Results.List {
          t := pass.TypesInfo.TypeOf(res.Type)
          if named, ok := t.(*types.Named); ok {
              if _, isIface := named.Underlying().(*types.Interface); isIface && named.Obj().Name() != "error" {
                  astutil.Report(pass, res.Pos(), "typedesign-04", "function %s returns interface type %s, return the concrete type instead", fd.Name.Name, named.Obj().Name())
              }
          }
      }
  })
  ```
  Exclusion: skip `error`. Skip when the returned interface type is unexported and declared in the same package as the function (the "hide the concrete type behind an unexported interface" constructor idiom is a recognized, deliberate pattern, not the rule's target).
- **Message**: `"function %s returns interface type %s, return the concrete type instead"`
- **Severity**: `SeverityWarning`.

### typedesign-05 — pointer-to-interface

- **Quote**: "Never use a pointer to an interface. Interfaces already hold a pointer to the underlying value."
- **Checkable because**: `*ast.StarExpr` wrapping a type whose `types.Underlying()` is `*types.Interface` is a direct semantic fact from the type checker, zero ambiguity.
- **Predicate** — node: `*ast.StarExpr` (in param lists, results, struct fields, var decls).
  ```go
  insp.Preorder([]ast.Node{(*ast.StarExpr)(nil)}, func(n ast.Node) {
      se := n.(*ast.StarExpr)
      if t := pass.TypesInfo.TypeOf(se.X); t != nil {
          if _, ok := t.Underlying().(*types.Interface); ok {
              astutil.Report(pass, se.Pos(), "typedesign-05", "pointer to interface type %s in %s, pass the interface by value", t.String(), pass.Pkg.Name())
          }
      }
  })
  ```
  Exclusion: none — a pointer to an interface type is never correct Go, no legitimate counter-case.
- **Message**: `"pointer to interface type %s in %s, pass the interface by value"`
- **Severity**: `SeverityWarning` — usually a design smell, occasionally masks a real bug (unwanted double indirection), but doesn't panic by itself.

### typedesign-06 — missing-compile-time-assertion

- **Quote**: "Verify interface compliance at compile time: `var _ Iface = (*T)(nil)`. Silent drift becomes a compile error."
- **Checkable because**: "T is passed somewhere as interface I" (evidence of intended satisfaction) combined with "no `var _ I = (*T)(nil)`-shaped statement exists in the package" is a syntactic + type-fact combination — no opinion about whether compliance *should* be verified, only whether it *is*.
- **Predicate** — nodes: call sites where an argument's static type is `T`/`*T` and the parameter type is a named interface `I`; then scan `*ast.ValueSpec` for the blank-identifier assertion shape.
  ```go
  // 1. collect (T, I) pairs where T is passed where I is expected
  pairs := map[*types.Named]*types.Interface{} // T -> I, evidence collected via call-site scan
  insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
      call := n.(*ast.CallExpr)
      sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
      if !ok {
          return
      }
      for i, arg := range call.Args {
          if i >= sig.Params().Len() {
              break
          }
          iface, ok := sig.Params().At(i).Type().Underlying().(*types.Interface)
          if !ok {
              continue
          }
          switch t := pass.TypesInfo.TypeOf(arg).(type) {
          case *types.Named:
              pairs[t] = iface
          case *types.Pointer:
              if named, ok := t.Elem().(*types.Named); ok {
                  pairs[named] = iface
              }
          }
      }
  })

  // 2. for each pair, scan file-level GenDecls for the assertion; report if missing
  for T, I := range pairs {
      found := false
      for _, f := range pass.Files {
          for _, decl := range f.Decls {
              genDecl, ok := decl.(*ast.GenDecl)
              if !ok || genDecl.Tok != token.VAR {
                  continue
              }
              for _, spec := range genDecl.Specs {
                  vs, ok := spec.(*ast.ValueSpec)
                  if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "_" || len(vs.Values) != 1 {
                      continue
                  }
                  if iface, ok := pass.TypesInfo.TypeOf(vs.Type).Underlying().(*types.Interface); ok && iface == I && matchesAssertedType(vs.Values[0], T) {
                      found = true
                  }
              }
          }
      }
      if !found {
          astutil.Report(pass, T.Obj().Pos(), "typedesign-06", "type %s satisfies interface %s with no compile-time assertion var _ %s = (*%s)(nil)", T.Obj().Name(), I.String(), I.String(), T.Obj().Name())
      }
  }
  ```
  Report position is `T.Obj().Pos()` — the satisfying type's own declaration line, not the interface's or the call site's; this is the one position both phases share unconditionally.
  Exclusion: only fire when at least one call site in the package supplies evidence T is meant to satisfy I (see step 1) — prevents flagging every struct against every interface in scope, which would be unbounded noise.
- **Message**: `"type %s satisfies interface %s with no compile-time assertion var _ %s = (*%s)(nil)"`
- **Severity**: `SeverityWarning` — missing safety net, not itself a defect.

### typedesign-07 — embedded-field-in-exported-struct

- **Quote**: "Never embed types in public structs. Embedding promotes methods into the public API, which cannot shrink without a break."
- **Checkable because**: an anonymous struct field (`ast.Field.Names == nil`) inside a struct type whose name starts with an uppercase letter is a pure syntactic/name-casing check.
- **Why this rule exists (and why it's narrowed)**: embedding any type into an exported struct promotes that type's method set into the public API — that's the general concern this rule checks. Embedding a `sync.*` primitive is a *different* concern (unsafe copy / accidental Lock/Unlock exposure on a concurrency-safety-critical type) that `concurrency-08` already owns. Without this exclusion, `type Cache struct { sync.Mutex }` fires both `typedesign-07` and `concurrency-08` on the identical line. This rule's job is embedding-in-general; `concurrency-08`'s job is sync-primitive embedding specifically. See `phase-4a-concurrency.md`, rule `concurrency-08`.
- **Predicate** — node: `*ast.StructType` reached from an exported `*ast.TypeSpec`.
  ```go
  insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
      ts := n.(*ast.TypeSpec)
      if !ts.Name.IsExported() {
          return
      }
      st, ok := ts.Type.(*ast.StructType)
      if !ok {
          return
      }
      for _, f := range st.Fields.List {
          if len(f.Names) != 0 { // has a name == not embedded
              continue
          }
          if isSyncType(pass.TypesInfo.TypeOf(f.Type)) {
              continue // concurrency-08's territory, not this rule's
          }
          astutil.Report(pass, f.Pos(), "typedesign-07", "exported struct %s embeds %s, use an unexported field with delegate methods", ts.Name.Name, pass.TypesInfo.TypeOf(f.Type).String())
      }
  })
  ```
  `isSyncType(t types.Type) bool` — narrowly, not "any type from a concurrency-related package": `t` is a `*types.Named` whose `Obj().Pkg().Path() == "sync"`. That is the entire predicate; it does not attempt to recognize third-party mutex-like types, because CONTRACTS' own exclusion-exhaustiveness rule (Section "Rule → AST-pattern conversion recipe", step 4) asks for exhaustive exclusions to prevent false positives here, not a speculative expansion of what this rule flags.
  Exclusion: skip embedded interfaces used purely for interface composition (`type ReadWriteCloser interface{ Reader; Writer; Closer }` — that's `*ast.InterfaceType`, not `*ast.StructType`, so it's already outside this predicate's node filter, not a special case). Skip embedded fields whose type is `sync.Mutex`, `sync.RWMutex`, `sync.WaitGroup`, or any other type declared in package `sync` — that is `concurrency-08`'s territory (see above); this rule fires only on non-sync embedded types.
- **Message**: `"exported struct %s embeds %s, use an unexported field with delegate methods"`
- **Severity**: `SeverityWarning` (per task's own worked example: exported-struct embedding = Warning) — irreversible API surface, but not a runtime bug at the point of writing.

### typedesign-08 — zero-value-enum-not-reserved

- **Quote**: "Reserve the zero value for `Unspecified`/`Unknown` and start real enum variants at 1. A zero-value enum is indistinguishable from 'not set' and silently passes validation."
- **Checkable because**: detecting an `iota`-based const group and checking whether its first identifier matches an "unset" naming allowlist is a name-pattern check over a syntactically-detectable const group — not a judgment call about whether the enum's zero value is "meaningfully" unset (that would be unchecked; the name-convention proxy is what's checked, and is stated as case-clarifying below).
- **Predicate** — node: `*ast.GenDecl` with `Tok == token.CONST`.
  ```go
  insp.Preorder([]ast.Node{(*ast.GenDecl)(nil)}, func(n ast.Node) {
      gd := n.(*ast.GenDecl)
      if gd.Tok != token.CONST || len(gd.Specs) < 2 {
          return
      }
      first := gd.Specs[0].(*ast.ValueSpec)
      if !usesIota(first) { // first.Values[0] is *ast.Ident "iota", or a later spec omits Values (implicit iota carry)
          return
      }
      name := first.Names[0].Name
      if !unsetNameRe.MatchString(name) { // (?i)unspecified|unknown|none|invalid|undefined
          astutil.Report(pass, first.Pos(), "typedesign-08", "enum %s starts at zero value %s with no unspecified/unset variant reserved", pass.TypesInfo.TypeOf(first.Names[0]).String(), name)
      }
  })
  ```
  Exclusion: skip groups with <2 specs (not a real enum, a lone sentinel). Skip untyped const groups with no explicit named type (bit-flag idiom `1 << iota` without a named `Status`-shaped type is a different pattern, rule 11's flag-set note, not this rule's target — detect via `first.Type == nil`).
- **Message**: `"enum %s starts at zero value %s with no unspecified/unset variant reserved"`
- **Severity**: `SeverityError` (per task's own worked example: zero-value-ambiguous enum = Error-adjacent) — an unset field silently validates as a real state; this is the exact shape of bug the reference calls "silently passes validation."
- **Cross-domain note**: absorbs the retired `naming-23` (`phase-4a-naming.md`) — that rule covered the same zero-value-enum-not-reserved case under a naming-convention framing; `typedesign-08`'s type-fact predicate is the semantically richer superset, so `naming-23` was excised rather than duplicated. `phase-4a-naming.md` cross-references back here.

### typedesign-09 — enum-missing-stringer

- **Quote**: "Give enums a `String()` method and stable wire serialization. Run `go generate` with `golang.org/x/tools/cmd/stringer`..."
- **Checkable because**: "named type with underlying `Int*` kind, backing a const group of ≥2 values" is a syntactic+type-fact enum definition; "no method named `String` with signature `() string`" is a direct `types.MethodSet` lookup — no interpretation needed.
- **Predicate** — nodes: `*types.Named` (from the const group's declared type) + method set lookup.
  ```go
  named := constGroupType.(*types.Named)
  if basic, ok := named.Underlying().(*types.Basic); !ok || basic.Info()&types.IsInteger == 0 {
      return
  }
  ms := types.NewMethodSet(named)
  if ms.Lookup(pass.Pkg, "String") == nil {
      astutil.Report(pass, named.Obj().Pos(), "typedesign-09", "enum type %s has no String() method", named.Obj().Name())
  }
  ```
  Exclusion: skip if the const group has <2 values (single sentinel constant, not an enum worth a Stringer).
- **Message**: `"enum type %s has no String() method"`
- **Severity**: `SeverityWarning` — debuggability/observability gap (raw ints in logs), not a correctness bug; the JSON-marshal-safety half of the source rule is excluded below (typedesign-excl-04) rather than folded in here.

### typedesign-10 — inconsistent-json-tags

- **Quote**: "All marshaled struct fields must have explicit tags... PascalCase field names couple your wire format to Go naming, so a rename silently breaks the wire."
- **Checkable because**: "does this struct literal have a `json:` tag on at least one field" and "does another exported field in the same struct have zero tag" are both syntactic facts read off `*ast.Field.Tag`, no inference about intent needed once framed as an internal-consistency check rather than a universal-tagging mandate.
- **Predicate** — node: `*ast.StructType`.
  ```go
  insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
      ts := n.(*ast.TypeSpec)
      st, ok := ts.Type.(*ast.StructType)
      if !ok {
          return
      }
      hasJSONTag, untaggedExported := false, []*ast.Field{}
      for _, f := range st.Fields.List {
          if len(f.Names) == 0 || !f.Names[0].IsExported() {
              continue // embedded fields are typedesign-07's territory
          }
          tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
          switch {
          case f.Tag == nil:
              untaggedExported = append(untaggedExported, f)
          case tag.Get("json") != "":
              hasJSONTag = true
          }
      }
      if hasJSONTag {
          for _, f := range untaggedExported {
              astutil.Report(pass, f.Pos(), "typedesign-10", "field %s in struct %s has no json tag while sibling fields do", f.Names[0].Name, ts.Name.Name)
          }
      }
  })
  ```
  Exclusion: skip unexported fields (never marshaled by `encoding/json`). Skip embedded/anonymous fields (rule 07's territory, avoid double-report). A struct with zero `json:` tags anywhere is not flagged at all — this predicate only catches *inconsistency*, not "should this struct be JSON-tagged," which would require knowing whether the struct crosses a wire boundary.
- **Message**: `"field %s in struct %s has no json tag while sibling fields do"`
- **Severity**: `SeverityWarning` — latent wire-compat risk, but only manifests given an actual consumer mismatch; not guaranteed-broken at write time the way `typedesign-17` is.

### typedesign-11 — primitive-type-alias

- **Quote**: "Use defined types for domain primitives, not aliases. `type UserID string` (defined...) documents intent... `type UserID = string` (alias) is interchangeable and adds no safety."
- **Checkable because**: `type X = Y` vs `type X Y` is a single token fact — `ast.TypeSpec.Assign` is a non-`token.NoPos` value if and only if the source used `=`. Whether `Y`'s underlying kind is a basic primitive is a `types.Basic` check.
- **Predicate** — node: `*ast.TypeSpec`.
  ```go
  insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
      ts := n.(*ast.TypeSpec)
      if ts.Assign == token.NoPos || !ts.Name.IsExported() {
          return // not an alias, or not part of the exported (domain-facing) API
      }
      if basic, ok := pass.TypesInfo.TypeOf(ts.Type).Underlying().(*types.Basic); ok {
          astutil.Report(pass, ts.Pos(), "typedesign-11", "type alias %s = %s hides a domain primitive behind an interchangeable alias, use a defined type", ts.Name.Name, basic.String())
      }
  })
  ```
  Exclusion: skip aliases whose target underlying is an interface or func type (legitimate deprecation/migration aliasing, not a "domain primitive" masquerade). Skip unexported alias names (internal shorthand, not public domain-type risk).
- **Message**: `"type alias %s = %s hides a domain primitive behind an interchangeable alias, use a defined type"`
- **Severity**: `SeverityWarning` — type-safety loss, not an active bug at declaration time.

### typedesign-12 — mixed-receiver-types

- **Quote**: "Pick pointer or value receivers deliberately and stay consistent... Never mix the two on one type."
- **Checkable because**: for a given named type, every method's receiver is syntactically either `(t T)` (`*ast.Ident`) or `(t *T)` (`*ast.StarExpr`) — collecting both shapes across all `*ast.FuncDecl.Recv` for the same base type name is a pure grouping operation.
- **Predicate** — node: `*ast.FuncDecl` with non-nil `Recv`.
  ```go
  recvKind := map[string]map[string]bool{}   // typeName -> {"ptr","val"} seen
  recvFirstPos := map[string]token.Pos{}     // typeName -> first-seen method's Pos (used as the report anchor)
  methodNames := map[string][]string{}       // typeName -> method names seen, for the message
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if fd.Recv == nil {
          return
      }
      typeName, isPtr := recvBaseName(fd.Recv.List[0].Type) // unwraps *ast.StarExpr if present
      if exempt[fd.Name.Name] { // "String","Error","MarshalJSON","UnmarshalJSON"
          return
      }
      if recvKind[typeName] == nil {
          recvKind[typeName] = map[string]bool{}
          recvFirstPos[typeName] = fd.Pos()
      }
      recvKind[typeName][kindOf(isPtr)] = true
      methodNames[typeName] = append(methodNames[typeName], fd.Name.Name)
  })
  // after the full pass: for each typeName with both "ptr" and "val" set, report once,
  // anchored at that type's first-seen mixed method (recvFirstPos) — not the type decl itself,
  // since a type can be declared in a different file than any of its methods.
  for typeName, kinds := range recvKind {
      if kinds["ptr"] && kinds["val"] {
          astutil.Report(pass, recvFirstPos[typeName], "typedesign-12", "type %s mixes pointer and value receivers across methods %s", typeName, strings.Join(methodNames[typeName], ", "))
      }
  }
  ```
  Exclusion: exempt `String`/`Error`/`MarshalJSON`/`UnmarshalJSON` — the idiomatic case of a value-receiver Stringer/Marshaler coexisting with pointer-receiver mutators is a recognized, common exception, not the drift this rule targets.
- **Message**: `"type %s mixes pointer and value receivers across methods %s"`
- **Severity**: `SeverityWarning` — breaks method-set consistency (can silently fail interface satisfaction), but requires a downstream interface-assignment to manifest as a build break.

### typedesign-13 — value-receiver-on-unsafe-copy-type

- **Quote**: "Pointer receiver when... the type holds a `sync.Mutex` or unbuffered channel (not safely copyable)."
- **Checkable because**: "struct has a field of type `sync.Mutex`/`sync.RWMutex`" is a direct type-identity check (`types.Object.Pkg().Path() == "sync"` + type name), and "method uses a value receiver" is the same receiver-shape check as rule 12 — combining two independently-checkable facts.
- **Predicate** — nodes: `*ast.StructType` fields + `*ast.FuncDecl.Recv`.
  ```go
  mutexTypes := map[string]bool{} // typeName -> has a sync.Mutex/RWMutex field
  insp.Preorder([]ast.Node{(*ast.TypeSpec)(nil)}, func(n ast.Node) {
      ts := n.(*ast.TypeSpec)
      st, ok := ts.Type.(*ast.StructType)
      if !ok {
          return
      }
      for _, f := range st.Fields.List {
          if isMutexOrRWMutex(pass.TypesInfo.TypeOf(f.Type)) {
              mutexTypes[ts.Name.Name] = true
          }
      }
  })
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if fd.Recv == nil {
          return
      }
      typeName, isPtr := recvBaseName(fd.Recv.List[0].Type) // shared with typedesign-12
      if mutexTypes[typeName] && !isPtr {
          astutil.Report(pass, fd.Pos(), "typedesign-13", "type %s holds a sync.Mutex/RWMutex field but method %s uses a value receiver", typeName, fd.Name.Name)
      }
  })
  ```
  Two flat `Preorder` passes over the whole file set (struct-field scan, then receiver scan) replace the original `methodsOf`/`isPointerRecv` sketch — both were undeclared project-wide symbols. `isMutexOrRWMutex` and `recvBaseName` are the only helpers needed, and `recvBaseName` is already declared for `typedesign-12`'s reuse, not duplicated.
  Exclusion: none — copying a struct containing `sync.Mutex` is never safe; `go vet`'s own `copylocks` analyzer treats this identically with no carve-outs.
- **Message**: `"type %s holds a sync.Mutex/RWMutex field but method %s uses a value receiver"`
- **Severity**: `SeverityError` — silent lock-copy is a genuine concurrency correctness bug (two goroutines locking independent copies), the same class of production incident go-security.md's concurrency rules exist to prevent. Struct-field-alignment/padding detection (also mentioned alongside receivers in the source) is deliberately excluded here — see `typedesign-excl-08`.

### typedesign-14 — exported-getter-returns-mutable-internal

- **Quote**: "For exported struct fields holding slices or maps, copy on set and on return... A returned internal slice is a live pointer into your struct, so `append` on the caller side corrupts your state."
- **Checkable because**: "exported method whose entire body is one `return recv.field` statement, where `field`'s type is a slice or map" is a syntactic shape (single-statement body + selector expression) combined with a type-kind check — no inference about caller intent required, only about the shape of the getter itself.
- **Predicate** — node: `*ast.FuncDecl` with `Recv != nil`.
  ```go
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if fd.Recv == nil || len(fd.Body.List) != 1 {
          return
      }
      ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
      if !ok || len(ret.Results) != 1 {
          return
      }
      sel, ok := ret.Results[0].(*ast.SelectorExpr)
      if !ok || !fd.Name.IsExported() {
          return
      }
      switch pass.TypesInfo.TypeOf(sel).Underlying().(type) {
      case *types.Slice:
          astutil.Report(pass, fd.Pos(), "typedesign-14", "method %s returns internal %s field %s without copying, callers can mutate shared state", fd.Name.Name, "slice", sel.Sel.Name)
      case *types.Map:
          astutil.Report(pass, fd.Pos(), "typedesign-14", "method %s returns internal %s field %s without copying, callers can mutate shared state", fd.Name.Name, "map", sel.Sel.Name)
      }
  })
  ```
  Exclusion: skip if the return expression is a call (e.g. `slices.Clone(recv.field)`, `maps.Clone(recv.field)`) rather than a bare selector — that IS the copy-on-return the rule asks for, and the "single-statement body" shape still matches but the inner expression is `*ast.CallExpr`, not `*ast.SelectorExpr`, so the type switch above naturally excludes it.
- **Message**: `"method %s returns internal %s field %s without copying, callers can mutate shared state"`
- **Severity**: `SeverityWarning` — becomes a real bug only if a caller actually mutates the aliased slice/map; the static check can't confirm that half, so it's flagged as risk rather than confirmed defect.

### typedesign-15 — trivial-constructor-wrapper

- **Quote**: "skip `NewT()` wrappers that only zero-init... if zero is invalid, make construction the only path."
- **Checkable because**: "zero-parameter function named `New*` whose body is exactly one `return &T{}` (or `return T{}`) with an empty composite literal" is a pure syntactic shape — no parameters means no construction logic could exist to justify the wrapper.
- **Predicate** — node: `*ast.FuncDecl`.
  ```go
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if !strings.HasPrefix(fd.Name.Name, "New") || fd.Type.Params.NumFields() != 0 {
          return
      }
      if len(fd.Body.List) != 1 {
          return
      }
      ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
      if !ok || len(ret.Results) != 1 {
          return
      }
      lit := unwrapAddr(ret.Results[0]) // strips *ast.UnaryExpr{Op: token.AND} if present
      if cl, ok := lit.(*ast.CompositeLit); ok && len(cl.Elts) == 0 {
          typeName := types.ExprString(cl.Type)
          astutil.Report(pass, fd.Pos(), "typedesign-15", "constructor %s only zero-initializes %s, remove the wrapper and use var %s", fd.Name.Name, typeName, typeName)
      }
  })
  ```
  Exclusion: only fires on zero-parameter constructors — any parameter implies real assignment/validation logic the AST shape can't rule out, so those are left alone rather than guessed at (the remainder of source Rule 15 — "validate required args in constructor," "Must only at startup" — is excluded below as judgment-dependent, `typedesign-excl-06`).
- **Message**: `"constructor %s only zero-initializes %s, remove the wrapper and use var %s"`
- **Severity**: `SeverityInfo` — style/dead-abstraction, no behavioral risk.

### typedesign-16 — single-call-site-generic

- **Quote**: "A function with one caller and one type pays that cost for nothing... Reach for generics when the same logic runs over two or more types."
- **Checkable because**: `*ast.FuncDecl.Type.TypeParams != nil` marks a generic function syntactically; counting distinct call/instantiation sites of an *unexported* identifier within a single package is a closed, countable set (unexported means no call site can exist outside the package, so the single-package pass's count is exhaustive, not a lower bound).
- **Predicate** — nodes: `*ast.FuncDecl` (generic) + `*ast.CallExpr`/`*ast.IndexExpr` referencing it.
  ```go
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      if fd.Type.TypeParams == nil || fd.Name.IsExported() {
          return // exported generics may have external callers a single-package pass can't see
      }
      fnObj := pass.TypesInfo.Defs[fd.Name]
      callSites := 0
      insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(m ast.Node) {
          if use, ok := pass.TypesInfo.Uses[m.(*ast.Ident)]; ok && use == fnObj {
              callSites++
          }
      })
      if callSites == 1 {
          astutil.Report(pass, fd.Pos(), "typedesign-16", "generic function %s has exactly one call site in this package, use a concrete type instead", fd.Name.Name)
      }
  })
  ```
  Object-identity resolution (`pass.TypesInfo.Uses[ident] == fnObj`, comparing `types.Object` pointers) replaces the earlier `calleeName(n.(*ast.CallExpr).Fun) == fd.Name.Name` string comparison — `calleeName` is on contracts.md's forbidden-name list and was also wrong on its own terms (a string match on the callee's source text doesn't survive a local shadow, a qualified selector, or a renamed import; identity resolution does).
  Exclusion: exported generics are skipped entirely — a single-package pass cannot prove "only one caller" for symbols visible to other packages, and asserting so would risk a false positive with no way to verify.
- **Message**: `"generic function %s has exactly one call site in this package, use a concrete type instead"`
- **Severity**: `SeverityInfo` — readability/maintenance concern, not correctness.

### typedesign-17 — nil-typed-pointer-as-interface

- **Quote**: "An interface is nil only when both its type and value are nil... Assigning a nil pointer of a concrete type produces a non-nil interface holding a nil value, so the caller's `err != nil` check fires on an empty error."
- **Checkable because**: the single-assignment case — `var p *T` (no `Values`) with zero subsequent reassignment before a `return p` in a function whose declared result type at that slot is a named interface — is fully determined by walking one function body's statement list, no whole-program dataflow needed for the common shape the source itself calls out.
- **Predicate** — nodes: `*ast.FuncDecl`, walking its body for `*ast.ReturnStmt`.
  ```go
  insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
      fd := n.(*ast.FuncDecl)
      ast.Inspect(fd.Body, func(m ast.Node) bool {
          ret, ok := m.(*ast.ReturnStmt)
          if !ok {
              return true
          }
          for i, expr := range ret.Results {
              id, ok := expr.(*ast.Ident)
              if !ok {
                  continue
              }
              resultType := pass.TypesInfo.TypeOf(funcResultTypeExpr(fd, i))
              iface, isIface := resultType.Underlying().(*types.Interface)
              if !isIface || iface == nil {
                  continue
              }
              declType := pass.TypesInfo.TypeOf(id)
              ptr, isPtr := declType.(*types.Pointer)
              if isPtr && declaredNilNoReassign(fd.Body, id) {
                  astutil.Report(pass, ret.Pos(), "typedesign-17", "function %s returns nil pointer %s of type %s as interface %s, callers see a non-nil interface", fd.Name.Name, id.Name, ptr.Elem().String(), resultType.String())
              }
          }
          return true
      })
  })
  ```
  `declaredNilNoReassign` walks `fd.Body.List` once, matching `var <id> *T` (no `Values`) with no intervening `*ast.AssignStmt` targeting `<id>` before the `return`.
  Exclusion: only fires on the direct single-assignment case within one function body — any reassignment, any conditional branch reaching the `var` differently, or the identifier passing through another function first is left unflagged (conservative: under-report rather than guess at a full points-to analysis).
- **Message**: `"function %s returns nil pointer %s of type %s as interface %s, callers see a non-nil interface"`
- **Severity**: `SeverityError` — the reference's own Production Note: this shipped a week of silent `err != nil` false-positives in production. Canonical high-severity Go footgun.

## Excluded (judgment call or out-of-scope, not converted into a fragile heuristic)

| ID | Source content | Rationale |
|---|---|---|
| `typedesign-excl-01` | Rule 3 — "keep interfaces as small as the consumer needs" | No objective method-count cutoff exists; any threshold (3? 5?) is invented, not sourced, and penalizes legitimately cohesive interfaces. |
| `typedesign-excl-02` | Rule 9 — "compose small interfaces instead of growing one" | Same size judgment as Rule 3, plus "growing" implies temporal drift across commits that a single-snapshot AST pass cannot observe. |
| `typedesign-excl-03` | Rule 10 — "omit zero-value fields only when zero is unambiguously absent" | "Unambiguously absent" is a domain-semantics judgment (what does zero mean for *this* field) with no syntactic proxy. |
| `typedesign-excl-04` | Rule 12 (second half) — JSON marshal/unmarshal must "reject unknown values" | Requires evaluating the *correctness* of hand-written `UnmarshalJSON` control flow — a semantic/behavioral judgment, not a pattern match. The `String()`-presence half is kept as `typedesign-09`. |
| `typedesign-excl-05` | Rule 14 — functional options for "three or more optional arguments" | Go has no optional-parameter syntax; "optional" isn't statically determinable, and param-count alone is a fragile proxy that flags required multi-field constructors as false positives. |
| `typedesign-excl-06` | Rule 15 (remainder) — `MustNew` only at startup; validate required args in constructor; zero value should be usable | Each requires knowing call-site context ("is this startup?"), business meaning of "required," or semantic usability — none are AST-visible. The one syntactically-closed sub-case (trivial zero-init wrapper) is kept as `typedesign-15`. |
| `typedesign-excl-07` | Rule 19 — generics constraint vocabulary (`comparable`, `cmp.Ordered`, custom `~` constraints) | Purely descriptive/pedagogical; no violation shape to detect, nothing to flag. |
| `typedesign-excl-08` | Rule 17 — struct field alignment/padding order | Checkable, but explicitly out of scope: contracts.md's dependency list already vendors `golang.org/x/tools/go/analysis/passes/fieldalignment` for this exact check — duplicating it here would double-report identical findings under two different `Finding.Rule` values. |

Excluded count: 8. Checkable rule count: 17.

## Section 3 — Fixture file spec

Location: `internal/tools/testdata/fixtures/audit-typedesign/`. Per contracts.md's testdata-fixture-layout contract (one isolated package per rule): each rule gets its own directory `rule<NN>/` (zero-padded to 2 digits), its own Go package `package rule<NN>`, and its own `violation.go` + (where a false-positive risk exists) `compliant.go` — never a mixed file with both. Every violation line carries `// VIOLATION: typedesign-NN` directly above it; every negative/near-miss line carries `// COMPLIANT: typedesign-NN` above it.

**Cross-rule contamination inside one directory, disclosed up front**: every `rule<NN>/` package is compiled and scanned by all 17 of this domain's rules, not just rule `NN` — so a fixture built to trigger rule `NN` cleanly can, and in several cases does, also trigger a sibling rule that shares the same minimal AST shape (an interface with a same-package implementer is simultaneously typedesign-02's and typedesign-03's target; an enum lacking an unset-name first const is simultaneously typedesign-08's target and, if it also lacks a `String()` method, typedesign-09's). Rather than contort fixture shapes to dodge every sibling rule (renaming enum constants, splitting implementers into `_test.go`, adding decoy methods), Section 5's tests isolate the rule under test via `astutil.FindingsForRule` — see Section 5 for the exact pattern and the per-rule contamination table.

Where a shared symbol is needed by both `violation.go` and `compliant.go` in the same directory (e.g. an interface used by both the violating and the compliant call site), the symbol is declared once, in `violation.go`, and referenced from `compliant.go` — same package, no duplicate declaration.

### Rule → directory map

| Rule ID | Directory | Violation symbol | Compliant/near-miss symbol | Anchor line (violation.go) |
|---|---|---|---|---|
| typedesign-01 | `rule01/` | `LegacyPayload` | `Streamer` (non-empty interface literal) | 4 |
| typedesign-02 | `rule02/` (+ `compliant_test.go`) | `Reader`/`fileReader` | `Writer` (impl only in `_test.go`, mock exception) | 4 |
| typedesign-03 | `rule03/` | `Closer`/`netConn` | `Formatter` (2 implementers same package — multi-impl guard) | 4 |
| typedesign-04 | `rule04/` | `NewRunner`→`Runner` | `newInternalRunner`→`runner` (unexported ctor-idiom exclusion) | 8 |
| typedesign-05 | `rule05/` | `ProcessBad(*Fetcher)` | `ProcessGood(*fetcherImpl)` (pointer to concrete type) | 12 |
| typedesign-06 | `rule06/` | `useStrict`/`strictValidator` | `useLenient`/`lenientValidator` + `var _ Validator = (*lenientValidator)(nil)` | 10 |
| typedesign-07 | `rule07/` | `Cache` (embeds `bytes.Buffer`) | `SafeCache` (named field `buf`); `WorkerPool` (embeds `sync.WaitGroup`, sync-exclusion near-miss) | 7 |
| typedesign-08 | `rule08/` | `Status`/`StatusActive` | `JobState`/`JobStateUnspecified` | 7 |
| typedesign-09 | `rule09/` | `Priority` (no `String()`) | `Tier` (has `String()`) | 4 |
| typedesign-10 | `rule10/` | `Settings` (mixed tagged/untagged) | `InternalConfig` (zero tags anywhere) | 6 |
| typedesign-11 | `rule11/` | `UserID` (`= string`) | `ConfigName` (`string`, defined not aliased) | 4 |
| typedesign-12 | `rule12/` | `Counter` (`Value` value-recv + `Increment` ptr-recv) | `Gauge` (`Set` ptr-recv + `String` value-recv, exempt name) | 6 |
| typedesign-13 | `rule13/` | `SafeCounter.Read` (value recv, has `sync.Mutex` field) | `LockedCounter.Read` (ptr recv) | 11 |
| typedesign-14 | `rule14/` | `Batch.Items` (bare `return b.items`) | `SafeBatch.Items` (`return slices.Clone(b.items)`) | 8 |
| typedesign-15 | `rule15/` | `NewWidget()` (zero-arg, `return &Widget{}`) | `NewNamedWidget(name string)` (has param) | 8 |
| typedesign-16 | `rule16/` | `firstOf[T]` (1 call site) | `lastOf[T]` (2 call sites) | 5 |
| typedesign-17 | `rule17/` | `ValidateBad` (`var verr *ValidationError`, no reassign, returned as `error`) | `ValidateGood` (`verr` reassigned to non-nil before return) | 13 |

17/17 checkable rules covered. 18 physical files (17 `violation.go` + 16 `compliant.go`, since typedesign-05's exclusion has no legitimate counter-case per Section 2, plus `rule02/compliant_test.go`).

### `rule01/` — typedesign-01

`violation.go`:
```go
package rule01

// VIOLATION: typedesign-01
var LegacyPayload interface{}
```
Anchor: `it.Pos()` (the `*ast.InterfaceType` node) lands on line 4, the `interface{}` token.

`compliant.go`:
```go
package rule01

// COMPLIANT: typedesign-01
// Non-empty interface literal — same *ast.InterfaceType node kind as the
// violation in violation.go, but Methods.List is non-empty, so the
// len==0 predicate must not fire.
var Streamer interface {
	Stream() ([]byte, error)
}
```

### `rule02/` — typedesign-02

`violation.go`:
```go
package rule02

// VIOLATION: typedesign-02
type Reader interface {
	Read() ([]byte, error)
}

type fileReader struct{ path string }
func (f *fileReader) Read() ([]byte, error) { return nil, nil }
```
Anchor: `typeSpec.Pos()` is `*ast.TypeSpec.Pos()`, which returns `Name.Pos()` — the `Reader` identifier on line 4.
Contamination: `Closer`... n/a here; this shape (interface + 1 same-package implementer) is also typedesign-03's minimal trigger — `fileReader` is `Reader`'s only implementer in this package, so typedesign-03 (`count<=1`) fires on the same package too. Isolated in Section 5 via `FindingsForRule`.

`compliant.go`:
```go
package rule02

// COMPLIANT: typedesign-02
// Writer's only same-package "implementer" is mockWriter, declared in
// compliant_test.go — the sanctioned mock-for-test exception, must not
// fire.
type Writer interface {
	Write(p []byte) (int, error)
}
```

`compliant_test.go`:
```go
package rule02

// mockWriter satisfies Writer (declared in compliant.go) but only from a
// _test.go file — the go-testing.md-sanctioned same-package mock
// exception. Must not make typedesign-02 fire on Writer.
type mockWriter struct{}

func (m *mockWriter) Write(p []byte) (int, error) { return len(p), nil }
```

### `rule03/` — typedesign-03

`violation.go`:
```go
package rule03

// VIOLATION: typedesign-03
type Closer interface {
	Close() error
}

type netConn struct{}

func (c *netConn) Close() error { return nil }
```
Anchor: `typeSpec.Pos()` = `Name.Pos()` — the `Closer` identifier on line 4.
Contamination: same shape as `rule02/violation.go` — `netConn` is `Closer`'s only same-package implementer, so typedesign-02 also fires here. Isolated via `FindingsForRule`.

`compliant.go`:
```go
package rule03

// COMPLIANT: typedesign-03
// Formatter has TWO implementers in this package — proves the multi-impl
// guard: count > 1 must suppress the finding.
type Formatter interface {
	Format() string
}

type jsonFormatter struct{}

func (j *jsonFormatter) Format() string { return "" }

type xmlFormatter struct{}

func (x *xmlFormatter) Format() string { return "" }
```
Note: `Formatter` still has same-package implementers, so it legitimately triggers typedesign-02 (producer-side-interface has no multi-impl exclusion) — that is expected and does not violate this rule's own `_CompliantIsSilent` test, which only checks for the absence of `typedesign-03` findings anchored at `compliant.go`.

### `rule04/` — typedesign-04

`violation.go`:
```go
package rule04

type Runner interface {
	Run() error
}

// VIOLATION: typedesign-04
func NewRunner() Runner { return nil }
```
Anchor: `res.Pos()` is the position of the result-type expression itself (the `Runner` token in the func signature), on line 8 — not the interface declaration's line.

`compliant.go`:
```go
package rule04

// COMPLIANT: typedesign-04
// runner is unexported and declared in the same package as its
// constructor — the "hide concrete type behind unexported interface"
// idiom, must not fire.
type runner interface {
	run() error
}

func newInternalRunner() runner { return nil }
```

### `rule05/` — typedesign-05

`violation.go`:
```go
package rule05

type Fetcher interface {
	Fetch() ([]byte, error)
}

type fetcherImpl struct{}

func (f *fetcherImpl) Fetch() ([]byte, error) { return nil, nil }

// VIOLATION: typedesign-05
func ProcessBad(f *Fetcher) {
	_ = f
}
```
Anchor: `se.Pos()` is the `*ast.StarExpr` node's position — the `*` of `*Fetcher` in the parameter list, on line 12.
Contamination: `Fetcher`/`fetcherImpl` is also the interface+1-same-package-implementer shape, so typedesign-02 and typedesign-03 both fire here too. Isolated via `FindingsForRule`.

`compliant.go`:
```go
package rule05

// COMPLIANT: typedesign-05
// Pointer to a concrete implementer, not to the interface itself — same
// *ast.StarExpr shape, underlying type is *types.Named struct, must not
// fire.
func ProcessGood(f *fetcherImpl) {
	_ = f
}
```
References `fetcherImpl` declared in `violation.go` — same package, no redeclaration.

### `rule06/` — typedesign-06

`violation.go`:
```go
package rule06

type Validator interface {
	Validate() error
}

func run(v Validator) { _ = v }

// VIOLATION: typedesign-06
type strictValidator struct{}

func (s *strictValidator) Validate() error { return nil }

func useStrict() {
	run(&strictValidator{})
}
```
Anchor: report position is `T.Obj().Pos()` per Section 2 — the satisfying type's own declaration line, `strictValidator` on line 10 — not `Validator`'s declaration line and not `useStrict`'s call site.
Contamination: `Validator`/`strictValidator` is again the interface+same-package-implementer shape (typedesign-02 fires). typedesign-03's `count<=1` does NOT fire once `compliant.go`'s `lenientValidator` is compiled into the same package (2 implementers total), which is itself worth noting as a directory-wide effect, not a per-file one.

`compliant.go`:
```go
package rule06

// COMPLIANT: typedesign-06
type lenientValidator struct{}

func (l *lenientValidator) Validate() error { return nil }

var _ Validator = (*lenientValidator)(nil)

func useLenient() {
	run(&lenientValidator{})
}
```
References `Validator` and `run` from `violation.go` — same package, no redeclaration.

### `rule07/` — typedesign-07

`violation.go`:
```go
package rule07

import "bytes"

// VIOLATION: typedesign-07
// bytes.Buffer is not a sync.* type — isSyncType returns false, so this
// embed is squarely typedesign-07's target (not concurrency-08's).
type Cache struct {
	bytes.Buffer // anonymous embedded field — promotes Read/Write/etc into the public API
	data map[string]string
}
```
Anchor: `f.Pos()` is the embedded field's position — the `bytes.Buffer` field line, line 7.

`compliant.go`:
```go
package rule07

import (
	"bytes"
	"sync"
)

// COMPLIANT: typedesign-07
// Named field buf, not embedded — same field type (bytes.Buffer), different
// f.Names shape, must not fire.
type SafeCache struct {
	buf  bytes.Buffer
	data map[string]string
}

// COMPLIANT: typedesign-07 (sync-exclusion near-miss)
// Anonymous embedded sync.WaitGroup — same f.Names==nil shape as the Cache
// violation in violation.go, but isSyncType(sync.WaitGroup) is true, so
// this rule must not fire; concurrency-08 owns this shape instead (see
// typedesign-07's "Why this rule exists" note in Section 2, and
// concurrency-08 in phase-4a-concurrency.md).
type WorkerPool struct {
	sync.WaitGroup
	size int
}
```

### `rule08/` — typedesign-08

`violation.go`:
```go
package rule08

type Status int

const (
	// VIOLATION: typedesign-08
	StatusActive Status = iota // zero value has no reserved unset/unspecified variant
	StatusInactive
	StatusSuspended
)
```
Anchor: `first.Pos()` is the first `*ast.ValueSpec`'s position — `StatusActive` on line 7.
Contamination: `Status` has no `String()` method either, so typedesign-09 also fires on this same type. Isolated via `FindingsForRule`.

`compliant.go`:
```go
package rule08

type JobState int

const (
	// COMPLIANT: typedesign-08
	JobStateUnspecified JobState = iota
	JobStateQueued
	JobStateComplete
)
```
Note: `JobState` also has no `String()` method, so typedesign-09 fires here too — expected, and irrelevant to this rule's own `_CompliantIsSilent` check (which is scoped to `typedesign-08` findings anchored at `compliant.go` — see Section 5).

### `rule09/` — typedesign-09

`violation.go`:
```go
package rule09

// VIOLATION: typedesign-09
type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
)
```
Anchor: `named.Obj().Pos()` is the named type's own declaration position — `Priority` on line 4.
Contamination: `PriorityLow` is not an "unset" name, so typedesign-08 also fires on this type. Isolated via `FindingsForRule`.

`compliant.go`:
```go
package rule09

// COMPLIANT: typedesign-09
type Tier int

const (
	TierBasic Tier = iota
	TierPremium
)

func (t Tier) String() string {
	if t == TierPremium {
		return "premium"
	}
	return "basic"
}
```
Note: `TierBasic` is also not an "unset" name, so typedesign-08 fires here too — expected; irrelevant to this rule's own `compliant.go`-scoped check.

### `rule10/` — typedesign-10

`violation.go`:
```go
package rule10

// VIOLATION: typedesign-10
type Settings struct {
	ID      string `json:"id"`
	Limit   int64 // exported, untagged, sibling ID has a json tag
}
```
Anchor: `f.Pos()` is the untagged field's position — `Limit int64` on line 6.

`compliant.go`:
```go
package rule10

// COMPLIANT: typedesign-10
// No field in this struct carries a json tag at all — the predicate only
// flags inconsistency (hasJSONTag==true + an untagged sibling), not
// "should this struct be tagged," so it must not fire.
type InternalConfig struct {
	Host string
	Port int
}
```

### `rule11/` — typedesign-11

`violation.go`:
```go
package rule11

// VIOLATION: typedesign-11
type UserID = string
```
Anchor: `ts.Pos()` = `Name.Pos()` — the `UserID` identifier on line 4.

`compliant.go`:
```go
package rule11

// COMPLIANT: typedesign-11
// Defined type, not an alias — ts.Assign is token.NoPos, must not fire.
type ConfigName string
```

### `rule12/` — typedesign-12

`violation.go`:
```go
package rule12

type Counter struct{ n int }

// VIOLATION: typedesign-12
func (c Counter) Value() int { return c.n }

func (c *Counter) Increment() { c.n++ }
```
Anchor: report position is `recvFirstPos[typeName]` per Section 2 — the first-seen (in source order) mixed-receiver method's `fd.Pos()` for `Counter`, which is `Value`'s func decl on line 6, not `Increment`'s.

`compliant.go`:
```go
package rule12

type Gauge struct{ n int }

func (g *Gauge) Set(v int) { g.n = v }

// COMPLIANT: typedesign-12
// String uses a value receiver alongside Gauge's pointer-receiver Set —
// exempted method name, must not fire the mixed-receiver check.
func (g Gauge) String() string { return "" }
```

### `rule13/` — typedesign-13

`violation.go`:
```go
package rule13

import "sync"

type SafeCounter struct {
	mu sync.Mutex
	n  int
}
// VIOLATION: typedesign-13
func (s SafeCounter) Read() int { return s.n } // value receiver copies the mutex
```
Anchor: `fd.Pos()` — the `Read` method's func decl on line 11. `mu sync.Mutex` is a *named* field (not embedded), so typedesign-07 does not fire here; `SafeCounter` has only one method, so typedesign-12's mixed-receiver check (which needs both a ptr- and a val-receiver method on the same type) does not fire either — this fixture is clean of sibling-rule contamination.

`compliant.go`:
```go
package rule13

import "sync"

type LockedCounter struct {
	mu sync.Mutex
	n  int
}

// COMPLIANT: typedesign-13
func (l *LockedCounter) Read() int { return l.n }
```

### `rule14/` — typedesign-14

`violation.go`:
```go
package rule14

type Batch struct {
	items []string
}

// VIOLATION: typedesign-14
func (b *Batch) Items() []string {
	return b.items
}
```
Anchor: `fd.Pos()` — the `Items` method's func decl on line 8.

`compliant.go`:
```go
package rule14

import "slices"

type SafeBatch struct {
	items []string
}

// COMPLIANT: typedesign-14
// Return expression is a *ast.CallExpr (slices.Clone), not a bare
// *ast.SelectorExpr — the type-switch on the return expr excludes it.
func (b *SafeBatch) Items() []string {
	return slices.Clone(b.items)
}
```

### `rule15/` — typedesign-15

`violation.go`:
```go
package rule15

type Widget struct {
	Name string
}

// VIOLATION: typedesign-15
func NewWidget() *Widget {
	return &Widget{}
}
```
Anchor: `fd.Pos()` — the `NewWidget` func decl on line 8.

`compliant.go`:
```go
package rule15

// COMPLIANT: typedesign-15
// Takes a parameter — real construction logic, excluded by the
// zero-parameter-only predicate.
func NewNamedWidget(name string) *Widget {
	return &Widget{Name: name}
}
```
References `Widget` declared in `violation.go` — same package, no redeclaration.

### `rule16/` — typedesign-16

`violation.go`:
```go
package rule16

// VIOLATION: typedesign-16
// firstOf has exactly one call site in this package (useFirstOf).
func firstOf[T any](s []T) T { return s[0] }

func useFirstOf() {
	_ = firstOf([]int{1, 2, 3})
}
```
Anchor: `fd.Pos()` — the `firstOf` func decl on line 5.

`compliant.go`:
```go
package rule16

// COMPLIANT: typedesign-16
// lastOf has two call sites in this package — above the single-call-site
// threshold, must not fire.
func lastOf[T any](s []T) T { return s[len(s)-1] }

func useLastOfA() {
	_ = lastOf([]int{1, 2, 3})
}

func useLastOfB() {
	_ = lastOf([]string{"a", "b"})
}
```

### `rule17/` — typedesign-17

`violation.go`:
```go
package rule17

type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// VIOLATION: typedesign-17
// var verr *ValidationError, no reassignment, returned into the error
// result — non-nil interface holding a nil value.
func ValidateBad(ok bool) error {
	var verr *ValidationError
	if ok {
		return nil
	}
	return verr
}
```
Anchor: `ret.Pos()` — the offending `return verr` statement, on line 13, not the `var verr` declaration.

`compliant.go`:
```go
package rule17

// COMPLIANT: typedesign-17
// verr IS reassigned to a non-nil value before the return that uses it —
// the no-reassignment precondition fails, must not fire.
func ValidateGood(ok bool) error {
	var verr *ValidationError
	if !ok {
		verr = &ValidationError{msg: "invalid"}
		return verr
	}
	return nil
}
```
References `ValidationError` declared in `violation.go` — same package, no redeclaration.

## Section 4 — Tool file spec

File: `internal/tools/go_audit_typedesign.go`, package `tools`. Analyzer: `internal/analysis/typedesign/` (per contracts.md "one subpackage per analysis domain" naming rule — not `internal/analysis/typedesign.go`, a stale single-file reference from an earlier draft). Cache: **not cached** — matches contracts.md TTL table row "every test/analysis tool (Tier 1, 2, 4 fuzz/pprof): 0 (never cached)"; this is an analysis tool.

Driver: `golang.org/x/tools/go/analysis/checker`, wired once in `internal/audit.Run` — this domain file does not reimplement `packages.Load`, `singlechecker`, or a hand-rolled `pass.Run` loop. Both blocks below are copied byte-for-byte from contracts.md's "Conformance block", substituting `<domain>` → `typedesign`, `<Domain>` → `Typedesign`.

### Analysis subpackage — `internal/analysis/typedesign/`

```go
package typedesign

import (
    "go/ast"

    "golang.org/x/tools/go/analysis"
    "golang.org/x/tools/go/analysis/passes/inspect"
    "golang.org/x/tools/go/ast/inspector"

    "github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
    astutil.RegisterRule("typedesign-01", "empty-interface-literal", finding.SeverityInfo)
    astutil.RegisterRule("typedesign-02", "producer-side-interface", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-03", "premature-interface", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-04", "interface-return-type", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-05", "pointer-to-interface", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-06", "missing-compile-time-assertion", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-07", "embedded-field-in-exported-struct", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-08", "zero-value-enum-not-reserved", finding.SeverityError)
    astutil.RegisterRule("typedesign-09", "enum-missing-stringer", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-10", "inconsistent-json-tags", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-11", "primitive-type-alias", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-12", "mixed-receiver-types", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-13", "value-receiver-on-unsafe-copy-type", finding.SeverityError)
    astutil.RegisterRule("typedesign-14", "exported-getter-returns-mutable-internal", finding.SeverityWarning)
    astutil.RegisterRule("typedesign-15", "trivial-constructor-wrapper", finding.SeverityInfo)
    astutil.RegisterRule("typedesign-16", "single-call-site-generic", finding.SeverityInfo)
    astutil.RegisterRule("typedesign-17", "nil-typed-pointer-as-interface", finding.SeverityError)
}

var Analyzer = &analysis.Analyzer{
    Name:     "typedesign",
    Doc:      "checks Go type-design conventions: interface placement, receiver consistency, enum safety",
    Run:      run,
    Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
    insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
    // The 17 Preorder/Inspect predicates from this file's Section 2 go here,
    // each ending in astutil.Report(pass, pos, "typedesign-NN", tmpl, args...).
    _ = insp
    return nil, nil // findings are collected via pass.Report -> Action.Diagnostics, not the return value
}
```

`Analyzer` is the **only** exported symbol from this subpackage — no `RunTypedesign`/`runTypedesign`/`mustRunTypedesign` wrapper, entry point, or driver.

### Tool file — `internal/tools/go_audit_typedesign.go`

```go
package tools

import (
    "context"
    "fmt"

    "golang.org/x/tools/go/analysis"

    "github.com/ashwingopalsamy/agentic-go/internal/analysis/typedesign"
    "github.com/ashwingopalsamy/agentic-go/internal/audit"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuditTypedesignInput struct {
    Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
    MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
    MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type AuditTypedesignOutput struct {
    Result finding.AuditResult `json:"result"`
}

func AuditTypedesignHandler(ctx context.Context, req *mcp.CallToolRequest, in AuditTypedesignInput) (*mcp.CallToolResult, AuditTypedesignOutput, error) {
    if err := normalizeAuditTypedesignInput(&in); err != nil {
        return nil, AuditTypedesignOutput{}, fmt.Errorf("validating input: %w", err)
    }
    ws, err := resolveInWorkspace(in.Package)
    if err != nil {
        return nil, AuditTypedesignOutput{}, fmt.Errorf("resolving package: %w", err)
    }
    result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{typedesign.Analyzer})
    if err != nil {
        return nil, AuditTypedesignOutput{}, fmt.Errorf("running typedesign audit: %w", err)
    }
    return nil, AuditTypedesignOutput{Result: result}, nil
}

func RegisterAuditTypedesign(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "go_audit_typedesign",
        Description: "checks Go type-design conventions: interface placement, receiver consistency, enum safety",
        Annotations: &mcp.ToolAnnotations{
            ReadOnlyHint:    true,
            DestructiveHint: boolPtr(false),
            IdempotentHint:  true,
            OpenWorldHint:   boolPtr(false),
        },
    }, AuditTypedesignHandler)
}

func normalizeAuditTypedesignInput(in *AuditTypedesignInput) error {
    if in.MaxFindings == 0 {
        in.MaxFindings = 200
    }
    if in.MaxFindings > 1000 {
        in.MaxFindings = 1000
    }
    if in.MinSeverity == "" {
        in.MinSeverity = finding.SeverityInfo
    }
    return nil // no field beyond defaulting needs validation for this domain
}
```

`resolveInWorkspace` is defined once, project-wide (contracts.md "Input containment and resource limits") — not redeclared here. `mcp.ToolAnnotations` fields above are the read-only-audit defaults per contracts.md "Tool annotations"; `go_audit_typedesign` doesn't override any of them, since it only reads and reports, never mutates source.

`packages.Load`'s mode (`NeedName|NeedFiles|NeedCompiledGoFiles|NeedImports|NeedDeps|NeedTypes|NeedTypesSizes|NeedTypesInfo|NeedSyntax`, `Tests: true`) and the `dedupeTestVariants` / `checker.Analyze` / `collect` pipeline live once in `internal/audit.Run` (contracts.md "Orchestration") — this tool file calls `audit.Run` and does not reimplement any of it. `NeedTypesSizes` is part of that fixed mode regardless of domain; none of typedesign's 17 rules currently reads `types.Sizes` directly, but the mode isn't narrowed per-domain since it's shared code in `audit.Run`, not per-tool config. `Tests: true` is what makes `typedesign-02`'s own exclusion (mock implementer declared only in a `_test.go` file) see `iface_placement_test.go`'s sanctioned mock — without it that exclusion silently breaks and the mock becomes a false positive.

### Rule granularity — `ast.Inspect` vs whole-package `go/types` fact pass

Only **typedesign-02, typedesign-03, typedesign-06** need the whole-package fact pass — each explicitly flags this limitation in its own Section 2 predicate (`typedesign-02`'s "Limitation (document, don't hide)" paragraph; `typedesign-03` reuses that same scan; `typedesign-06`'s two-phase "collect (T,I) pairs... then scan file-level GenDecls"). All other 14 rules resolve entirely from the node under visit (plus, for a few, other nodes gathered within the *same single* `Preorder` traversal — never `pass.Pkg.Scope()` enumeration or a `types.Implements` scan).

| Rule ID | Granularity | Why |
|---|---|---|
| typedesign-01 | `ast.Inspect` | Pure syntax: `*ast.InterfaceType.Methods.List` len, no type facts |
| typedesign-02 | **whole-package fact pass** | Enumerates `pass.Pkg.Scope().Names()`, runs `types.Implements` per candidate — not reachable from the interface's own node |
| typedesign-03 | **whole-package fact pass** | Reuses 02's full-scope implementer scan, extended to counting |
| typedesign-04 | `ast.Inspect` + node-local `pass.TypesInfo.TypeOf` | Result type resolved from the `*ast.FuncDecl.Type.Results` node itself |
| typedesign-05 | `ast.Inspect` + node-local `pass.TypesInfo.TypeOf` | `*ast.StarExpr` operand resolved locally |
| typedesign-06 | **whole-package fact pass** | Phase 1: whole-package call-site scan for (T,I) evidence. Phase 2: whole-package `GenDecl` scan for the assertion. Neither phase is node-local |
| typedesign-07 | `ast.Inspect` | Anonymous field + exported struct name, both on the node |
| typedesign-08 | `ast.Inspect` | Const-group shape + name regex, node-local |
| typedesign-09 | `ast.Inspect` node discovery + single `types.NewMethodSet` call per candidate type | Method-set lookup is per-type, not a package-scope enumeration |
| typedesign-10 | `ast.Inspect` | Single `*ast.StructType` node's own field list |
| typedesign-11 | `ast.Inspect` + node-local `pass.TypesInfo.TypeOf` | `ts.Assign` token + underlying kind, both on the node |
| typedesign-12 | `ast.Inspect` (map accumulated across one Preorder, reported after) | Aggregates `*ast.FuncDecl` receiver shapes only — no `types.Implements`, no scope scan |
| typedesign-13 | `ast.Inspect` + node-local `pass.TypesInfo.TypeOf` on struct fields | Mutex-field check is per-struct-node |
| typedesign-14 | `ast.Inspect` + `pass.TypesInfo.TypeOf` on the return selector | Single-function-body shape |
| typedesign-15 | `ast.Inspect` | Pure syntactic constructor-body shape |
| typedesign-16 | `ast.Inspect` (generic-decl Preorder + call-site-count Preorder, both bounded to the loaded package's own AST) | Callee is unexported ⇒ count is exhaustive without scope enumeration |
| typedesign-17 | `ast.Inspect` (single-function-body walk) + `pass.TypesInfo.TypeOf` | Section 2's own predicate text: "no whole-program dataflow needed" |

## Section 5 — Verification spec

File: `internal/analysis/typedesign/typedesign_test.go`, package `typedesign_test` (external test package — the analyzer's exported `Analyzer` is the only symbol these tests touch). One test function per rule, plus one whole-domain count test.

**Two deviations from contracts.md's literal §5 template, both forced by the same fact — every `rule<NN>/` fixture directory is one compiled package, and `astutil.RunFixture` returns every finding produced anywhere in that package, not just the ones belonging to the rule the directory is named after:**

1. **TP test** filters `findings` down to the target rule via `astutil.FindingsForRule` before asserting `require.Len(t, rf, 1)`. Several rule pairs structurally co-fire on the same minimal trigger shape (typedesign-02/03 on any same-package interface+single-implementer; typedesign-08/09 on any zero-started enum with no `String()`) — see Section 3's per-rule contamination notes. Asserting on the raw, unfiltered `findings` slice length would make the TP test's expected count depend on sibling rules' behavior, which is not what the test is verifying.
2. **`_CompliantIsSilent` test** filters `findings` to only those whose `Location.File` is the `compliant.go` path before asserting none of them carry the target rule ID. The literal template's "loop over all `findings`, assert none match" would fail immediately for every domain that follows the one-fixture-package-per-rule layout, since `violation.go`'s own genuine finding (the one the TP test just asserted exists) is a `findings` member that DOES match the target rule. Filtering by file is the only reading of "compliant.go must not trigger its own rule" that isn't self-contradictory.

```go
package typedesign_test

import (
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/typedesign"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditTypedesign_Rule01(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule01")
	rf := astutil.FindingsForRule(findings, "typedesign-01")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-01", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule01/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditTypedesign_Rule01_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule01")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule01/compliant.go" {
			assert.NotEqual(t, "typedesign-01", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule02(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule02")
	rf := astutil.FindingsForRule(findings, "typedesign-02")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-02", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule02/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditTypedesign_Rule02_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule02")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule02/compliant.go" {
			assert.NotEqual(t, "typedesign-02", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule03(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule03")
	rf := astutil.FindingsForRule(findings, "typedesign-03")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-03", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule03/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditTypedesign_Rule03_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule03")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule03/compliant.go" {
			assert.NotEqual(t, "typedesign-03", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule04(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule04")
	rf := astutil.FindingsForRule(findings, "typedesign-04")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-04", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule04/violation.go", f.Location.File)
	assert.Equal(t, 8, f.Location.Line)
}

func TestAuditTypedesign_Rule04_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule04")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule04/compliant.go" {
			assert.NotEqual(t, "typedesign-04", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule05(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule05")
	rf := astutil.FindingsForRule(findings, "typedesign-05")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-05", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule05/violation.go", f.Location.File)
	assert.Equal(t, 12, f.Location.Line)
}

func TestAuditTypedesign_Rule05_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule05")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule05/compliant.go" {
			assert.NotEqual(t, "typedesign-05", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}
func TestAuditTypedesign_Rule06(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule06")
	rf := astutil.FindingsForRule(findings, "typedesign-06")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-06", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule06/violation.go", f.Location.File)
	assert.Equal(t, 10, f.Location.Line)
}

func TestAuditTypedesign_Rule06_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule06")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule06/compliant.go" {
			assert.NotEqual(t, "typedesign-06", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule07(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule07")
	rf := astutil.FindingsForRule(findings, "typedesign-07")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-07", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule07/violation.go", f.Location.File)
	assert.Equal(t, 7, f.Location.Line)
}

func TestAuditTypedesign_Rule07_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule07")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule07/compliant.go" {
			assert.NotEqual(t, "typedesign-07", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule08(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule08")
	rf := astutil.FindingsForRule(findings, "typedesign-08")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-08", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule08/violation.go", f.Location.File)
	assert.Equal(t, 7, f.Location.Line)
}

func TestAuditTypedesign_Rule08_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule08")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule08/compliant.go" {
			assert.NotEqual(t, "typedesign-08", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule09(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule09")
	rf := astutil.FindingsForRule(findings, "typedesign-09")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-09", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule09/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditTypedesign_Rule09_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule09")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule09/compliant.go" {
			assert.NotEqual(t, "typedesign-09", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule10(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule10")
	rf := astutil.FindingsForRule(findings, "typedesign-10")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-10", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule10/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditTypedesign_Rule10_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule10")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule10/compliant.go" {
			assert.NotEqual(t, "typedesign-10", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule11(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule11")
	rf := astutil.FindingsForRule(findings, "typedesign-11")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-11", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule11/violation.go", f.Location.File)
	assert.Equal(t, 4, f.Location.Line)
}

func TestAuditTypedesign_Rule11_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule11")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule11/compliant.go" {
			assert.NotEqual(t, "typedesign-11", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule12(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule12")
	rf := astutil.FindingsForRule(findings, "typedesign-12")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-12", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule12/violation.go", f.Location.File)
	assert.Equal(t, 6, f.Location.Line)
}

func TestAuditTypedesign_Rule12_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule12")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule12/compliant.go" {
			assert.NotEqual(t, "typedesign-12", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule13(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule13")
	rf := astutil.FindingsForRule(findings, "typedesign-13")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-13", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule13/violation.go", f.Location.File)
	assert.Equal(t, 11, f.Location.Line)
}

func TestAuditTypedesign_Rule13_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule13")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule13/compliant.go" {
			assert.NotEqual(t, "typedesign-13", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule14(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule14")
	rf := astutil.FindingsForRule(findings, "typedesign-14")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-14", f.Rule)
	assert.Equal(t, finding.SeverityWarning, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule14/violation.go", f.Location.File)
	assert.Equal(t, 8, f.Location.Line)
}

func TestAuditTypedesign_Rule14_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule14")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule14/compliant.go" {
			assert.NotEqual(t, "typedesign-14", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule15(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule15")
	rf := astutil.FindingsForRule(findings, "typedesign-15")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-15", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule15/violation.go", f.Location.File)
	assert.Equal(t, 8, f.Location.Line)
}

func TestAuditTypedesign_Rule15_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule15")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule15/compliant.go" {
			assert.NotEqual(t, "typedesign-15", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule16(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule16")
	rf := astutil.FindingsForRule(findings, "typedesign-16")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-16", f.Rule)
	assert.Equal(t, finding.SeverityInfo, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule16/violation.go", f.Location.File)
	assert.Equal(t, 5, f.Location.Line)
}

func TestAuditTypedesign_Rule16_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule16")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule16/compliant.go" {
			assert.NotEqual(t, "typedesign-16", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_Rule17(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule17")
	rf := astutil.FindingsForRule(findings, "typedesign-17")
	require.Len(t, rf, 1)
	f := rf[0]
	assert.Equal(t, "typedesign-17", f.Rule)
	assert.Equal(t, finding.SeverityError, f.Severity)
	assert.Equal(t, "fixtures/audit-typedesign/rule17/violation.go", f.Location.File)
	assert.Equal(t, 13, f.Location.Line)
}

func TestAuditTypedesign_Rule17_CompliantIsSilent(t *testing.T) {
	findings := astutil.RunFixture(t, typedesign.Analyzer, "audit-typedesign/rule17")
	for _, f := range findings {
		if f.Location.File == "fixtures/audit-typedesign/rule17/compliant.go" {
			assert.NotEqual(t, "typedesign-17", f.Rule, "compliant.go must not trigger its own rule")
		}
	}
}

func TestAuditTypedesign_TotalRuleCount(t *testing.T) {
	assert.Len(t, astutil.RulesInDomain("typedesign"), 17)
}
```

17/17 checkable rules verified, each with one true-positive test and one `_CompliantIsSilent` test, both against the exact fixture directories, symbols, and report-position anchors from Section 3 — no additional fixtures introduced. Message-template and severity values used above are copied byte-for-byte from Section 2's per-rule detail; none are re-derived or guessed here.
