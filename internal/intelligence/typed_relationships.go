package intelligence

import (
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
)

const maxTypedRelationships = 100

// TypedEvidence contains Go-specific relationships supported by parsed source
// and, where required, resolved go/types objects.
type TypedEvidence struct {
	Relationships []GoRelationship `json:"relationships"`
	Uncertainties []Uncertainty    `json:"uncertainties"`
	Complete      bool             `json:"complete"`
	Truncated     bool             `json:"truncated"`
}

// GoRelationship records one bounded predicate and its source/build evidence.
//
//nolint:govet // Field order is the public JSON schema order.
type GoRelationship struct {
	Kind         string      `json:"kind"`
	Subject      string      `json:"subject"`
	Object       string      `json:"object"`
	Location     Location    `json:"location"`
	Evidence     string      `json:"evidence"`
	Build        BuildConfig `json:"build"`
	Availability string      `json:"availability"`
	Limits       []string    `json:"limits"`
}

//nolint:govet // Grouping follows the parser and type-checking phases.
type typedPackage struct {
	files      []*ast.File
	fset       *token.FileSet
	info       *types.Info
	pkg        *types.Package
	parseOK    bool
	typeOK     bool
	buildFiles map[string]bool
	relative   map[string]string
}

func (c *Core) typedEvidence(ctx context.Context, observation *snapshotObservation, symbol *SymbolContext, examples []SymbolMatch) TypedEvidence {
	result := TypedEvidence{Relationships: []GoRelationship{}, Uncertainties: []Uncertainty{}, Complete: true}
	if symbol == nil || symbol.Symbol.Location.File == "" {
		return result
	}
	loaded, uncertainties := c.loadTypedPackage(ctx, observation, symbol.Symbol.Location.File)
	result.Uncertainties = append(result.Uncertainties, uncertainties...)
	result.Complete = loaded.parseOK && loaded.typeOK
	if !loaded.parseOK {
		result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "typed.source_unavailable", Message: "typed relationships are unavailable because active source could not be parsed", Locations: []Location{symbol.Symbol.Location}})
		return result
	}
	anchor := lookupAnchorObject(loaded, symbol.Symbol)
	if anchor == nil {
		result.Complete = false
		result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "typed.anchor_unavailable", Message: "the selected declaration has no resolved go/types object", Locations: []Location{symbol.Symbol.Location}})
		return result
	}
	addLocation := func(kind, subject, object string, location Location, evidence string, limits ...string) {
		if len(result.Relationships) >= maxTypedRelationships {
			result.Truncated = true
			return
		}
		if location.File == "" {
			result.Complete = false
			result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "typed.source_unavailable", Message: "a typed relationship had no attributable workspace source location", Locations: []Location{}})
			return
		}
		result.Relationships = append(result.Relationships, GoRelationship{Kind: kind, Subject: subject, Object: object, Location: location, Evidence: evidence, Build: observation.snapshot.Build, Availability: "available", Limits: append([]string{}, limits...)})
	}
	add := func(kind, subject, object string, position token.Pos, evidence string, limits ...string) {
		addLocation(kind, subject, object, typedLocation(loaded, position), evidence, limits...)
	}
	addTypeClassification(loaded, anchor, add)
	addGenericOrigins(loaded, anchor, add)
	addMethodSets(loaded, anchor, add)
	addInterfaceObligations(loaded, anchor, add)
	addEmbedding(loaded, anchor, add)
	for _, implementation := range symbol.Implementations.Items {
		addLocation("existing_implementation", symbol.Symbol.Qualified, implementation.Qualified, implementation.Location, "semantic-provider implementation result", "static relationship; runtime dispatch may differ")
	}
	for _, example := range examples {
		if strings.HasPrefix(example.Name, "Example") {
			addLocation("typed_example", anchor.Name(), example.Qualified, example.Location, "referenced enclosing Go example declaration", "navigation evidence; not executed evidence")
		}
	}
	addLifecycleSites(loaded, anchor, add)
	sort.Slice(result.Relationships, func(i, j int) bool {
		left, right := result.Relationships[i], result.Relationships[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Location != right.Location {
			return locationLess(left.Location, right.Location)
		}
		if left.Subject != right.Subject {
			return left.Subject < right.Subject
		}
		return left.Object < right.Object
	})
	return result
}

func (c *Core) focusFileOrPackage(ctx context.Context, observation *snapshotObservation, request FocusRequest) (*FocusContext, error) {
	result := &FocusContext{Selection: FocusSelection{File: request.FocusFile, Package: request.FocusPackage}, Candidates: []SymbolMatch{}, TestDeclarations: []SymbolMatch{}, Excerpts: []SourceExcerpt{}, CallSites: []FocusCallSite{}, Reasons: []string{}, Uncertainties: []Uncertainty{}, EvidenceStates: []EvidenceState{}, TypedEvidence: &TypedEvidence{Relationships: []GoRelationship{}, Uncertainties: []Uncertainty{}, Complete: true}}
	packages, err := inventoryPackagesForObservation(ctx, c.workspace, c.runner, observation.snapshot.Scope, observation)
	if err != nil {
		return nil, err
	}
	var selected []inventoryPackage
	selectedFile := ""
	if request.FocusFile != "" {
		absolute, resolveErr := c.workspace.Resolve(request.FocusFile)
		if resolveErr != nil {
			return nil, resolveErr
		}
		selectedFile, resolveErr = c.workspace.Relative(absolute)
		if resolveErr != nil {
			return nil, resolveErr
		}
		for _, pkg := range packages {
			if filepath.Clean(pkg.Dir) == filepath.Dir(absolute) {
				selected = append(selected, pkg)
			}
		}
	} else {
		for _, pkg := range packages {
			directory, _ := c.workspace.Relative(pkg.Dir)
			if request.FocusPackage == pkg.ImportPath || request.FocusPackage == pkg.Name || request.FocusPackage == directory {
				selected = append(selected, pkg)
			}
		}
	}
	if len(selected) != 1 {
		return nil, fmt.Errorf("focus package selection resolved to %d active packages", len(selected))
	}
	pkg := selected[0]
	files := append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range files {
		absolute := filepath.Join(pkg.Dir, name)
		relative, relErr := c.workspace.Relative(absolute)
		if relErr != nil {
			return nil, relErr
		}
		if selectedFile != "" && relative != selectedFile {
			continue
		}
		source, sourceErr := observation.source(relative)
		if sourceErr != nil {
			return nil, sourceErr
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, absolute, source, parser.AllErrors)
		if parseErr != nil {
			result.TypedEvidence.Complete = false
			result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "typed.partial", Message: parseErr.Error(), Locations: []Location{{File: relative}}})
		}
		if file == nil {
			continue
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if len(result.Candidates) >= maxFocusCandidates {
					result.TypedEvidence.Truncated = true
					continue
				}
				position := fileSet.PositionFor(typeSpec.Name.Pos(), false)
				lsp, positionErr := sourcePositionFromContents(name, source, SourcePosition{File: relative, Line: position.Line, Column: position.Column})
				if positionErr != nil {
					continue
				}
				match := SymbolMatch{Name: typeSpec.Name.Name, Qualified: pkg.ImportPath + "." + typeSpec.Name.Name, Package: pkg.ImportPath, Kind: "go.type", Location: Location{File: relative, Line: position.Line, Column: position.Column, EndLine: position.Line, EndColumn: position.Column + len(typeSpec.Name.Name)}}
				match.Ref, _ = encodeSymbolRef(symbolIdentity{SnapshotID: observation.snapshot.ID, Base: observation.snapshot.RequestedBase, Scope: observation.snapshot.Scope, Path: relative, Kind: match.Kind, Package: match.Package, Qualified: match.Qualified, Position: lsp})
				result.Candidates = append(result.Candidates, match)
				typed := c.typedEvidence(ctx, observation, &SymbolContext{Symbol: match, Implementations: SymbolSet{Items: []SymbolMatch{}}, Uncertainties: []Uncertainty{}}, nil)
				result.TypedEvidence.Relationships = append(result.TypedEvidence.Relationships, typed.Relationships...)
				result.TypedEvidence.Uncertainties = append(result.TypedEvidence.Uncertainties, typed.Uncertainties...)
				result.TypedEvidence.Complete = result.TypedEvidence.Complete && typed.Complete
				result.TypedEvidence.Truncated = result.TypedEvidence.Truncated || typed.Truncated
			}
		}
	}
	sortSymbols(result.Candidates)
	if result.TypedEvidence.Truncated {
		result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_declarations", State: "gathered_but_omitted", Reason: "file or package declaration bound exceeded"})
	}
	if len(result.Candidates) == 0 {
		result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_declarations", State: "examined_and_absent"})
	} else {
		result.Reasons = append(result.Reasons, "selected active Go type declarations for the requested file or package")
	}
	if len(result.TypedEvidence.Relationships) > maxTypedRelationships {
		result.TypedEvidence.Relationships = result.TypedEvidence.Relationships[:maxTypedRelationships]
		result.TypedEvidence.Truncated = true
	}
	result.Uncertainties = append(result.Uncertainties, result.TypedEvidence.Uncertainties...)
	bounded, err := c.boundFocusContext(*result, request.MaxBytes)
	return &bounded, err
}

func (c *Core) loadTypedPackage(ctx context.Context, observation *snapshotObservation, anchorFile string) (typedPackage, []Uncertainty) {
	result := typedPackage{fset: token.NewFileSet(), info: &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}, parseOK: true, typeOK: true, buildFiles: map[string]bool{}, relative: map[string]string{}}
	packages, err := inventoryPackagesForObservation(ctx, c.workspace, c.runner, observation.snapshot.Scope, observation)
	if err != nil {
		return result, []Uncertainty{{Code: "typed.package_unavailable", Message: err.Error(), Locations: []Location{}}}
	}
	anchorAbsolute, err := c.workspace.Resolve(anchorFile)
	if err != nil {
		return result, []Uncertainty{{Code: "typed.package_unavailable", Message: err.Error(), Locations: []Location{}}}
	}
	var selected *inventoryPackage
	for i := range packages {
		if filepath.Clean(packages[i].Dir) == filepath.Dir(anchorAbsolute) {
			selected = &packages[i]
			break
		}
	}
	if selected == nil {
		result.parseOK = false
		result.typeOK = false
		return result, []Uncertainty{{Code: "typed.package_unavailable", Message: "selected file is not in the active package inventory", Locations: []Location{{File: anchorFile}}}}
	}
	for _, name := range append(append([]string{}, selected.GoFiles...), selected.CgoFiles...) {
		absolute := filepath.Join(selected.Dir, name)
		relative, relErr := c.workspace.Relative(absolute)
		if relErr != nil {
			result.parseOK = false
			continue
		}
		result.buildFiles[relative] = true
		result.relative[absolute] = relative
		source, sourceErr := observation.source(relative)
		if sourceErr != nil {
			result.parseOK = false
			continue
		}
		file, parseErr := parser.ParseFile(result.fset, absolute, source, parser.ParseComments|parser.AllErrors)
		if file != nil {
			result.files = append(result.files, file)
		}
		if parseErr != nil {
			result.parseOK = false
		}
	}
	typeErrors := []error{}
	config := types.Config{Importer: importer.Default(), Error: func(err error) { typeErrors = append(typeErrors, err) }}
	result.pkg, err = config.Check(selected.ImportPath, result.fset, result.files, result.info)
	if err != nil || len(typeErrors) > 0 {
		result.typeOK = false
	}
	uncertainties := []Uncertainty{}
	if !result.typeOK {
		uncertainties = append(uncertainties, Uncertainty{Code: "typed.partial", Message: "some typed relationships are unavailable because the active package did not type-check completely", Locations: []Location{}})
	}
	if len(selected.IgnoredGoFiles) > 0 {
		uncertainties = append(uncertainties, Uncertainty{Code: "typed.build_variants_unexamined", Message: "files excluded by the active build configuration were not examined", Locations: []Location{}})
	}
	return result, uncertainties
}

func lookupAnchorObject(pkg typedPackage, match SymbolMatch) types.Object {
	for ident, object := range pkg.info.Defs {
		if object == nil || ident.Name != match.Name {
			continue
		}
		location := typedLocation(pkg, ident.Pos())
		if location.File == match.Location.File {
			return object
		}
	}
	if pkg.pkg != nil {
		return pkg.pkg.Scope().Lookup(match.Name)
	}
	return nil
}

func addTypeClassification(_ typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	typeName, ok := anchor.(*types.TypeName)
	if !ok {
		return
	}
	if typeName.IsAlias() {
		add("type_alias", typeName.Name(), types.TypeString(typeName.Type(), qualifier), typeName.Pos(), "go/types TypeName.IsAlias", "alias identity only")
		if named, namedOK := types.Unalias(typeName.Type()).(*types.Named); namedOK && named.Origin() != named {
			add("generic_origin", typeName.Name(), named.Origin().Obj().Name(), typeName.Pos(), "go/types alias target Named.Origin", "type identity only")
		}
	} else {
		add("defined_type", typeName.Name(), types.TypeString(typeName.Type().Underlying(), qualifier), typeName.Pos(), "go/types defined type", "does not imply behavioral equivalence")
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		return
	}
	params := named.TypeParams()
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		add("generic_parameter", typeName.Name(), param.Obj().Name()+" "+types.TypeString(param.Constraint(), qualifier), param.Obj().Pos(), "resolved type parameter and constraint", "constraint satisfaction only")
	}
	if named.Origin() != named {
		add("generic_origin", typeName.Name(), named.Origin().Obj().Name(), typeName.Pos(), "go/types Named.Origin", "type identity only")
	}
}

func addGenericOrigins(pkg typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	typeName, ok := anchor.(*types.TypeName)
	if !ok {
		return
	}
	origin, ok := types.Unalias(typeName.Type()).(*types.Named)
	if !ok {
		return
	}
	origin = origin.Origin()
	for expression, typed := range pkg.info.Types {
		named, ok := types.Unalias(typed.Type).(*types.Named)
		if !ok || named == named.Origin() || named.Origin() != origin {
			continue
		}
		add("generic_origin", types.TypeString(named, qualifier), typeName.Name(), expression.Pos(), "resolved instantiated type and go/types Named.Origin", "type identity only")
	}
}

func addMethodSets(_ typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	typeName, ok := anchor.(*types.TypeName)
	if !ok {
		return
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		return
	}
	for _, item := range []struct {
		set  *types.MethodSet
		kind string
	}{{set: types.NewMethodSet(named), kind: "value_method"}, {set: types.NewMethodSet(types.NewPointer(named)), kind: "pointer_method"}} {
		for i := 0; i < item.set.Len(); i++ {
			selection := item.set.At(i)
			add(item.kind, typeName.Name(), selection.Obj().Name()+types.TypeString(selection.Obj().Type(), qualifier), selection.Obj().Pos(), "resolved go/types method set", "method-set membership does not prove runtime dispatch")
		}
	}
}

func addInterfaceObligations(pkg typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	if pkg.pkg == nil {
		return
	}
	typeName, ok := anchor.(*types.TypeName)
	if !ok {
		return
	}
	anchorInterface, selectedInterface := typeName.Type().Underlying().(*types.Interface)
	if selectedInterface {
		anchorInterface.Complete()
	}
	for _, name := range pkg.pkg.Scope().Names() {
		candidate, ok := pkg.pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || candidate == typeName {
			continue
		}
		if selectedInterface {
			if _, isInterface := candidate.Type().Underlying().(*types.Interface); !isInterface {
				if types.Implements(candidate.Type(), anchorInterface) {
					add("interface_implementation", candidate.Name(), typeName.Name(), candidate.Pos(), "go/types.Implements(value, selected interface)", "compile-time method-set predicate")
				}
				if types.Implements(types.NewPointer(candidate.Type()), anchorInterface) && !types.Implements(candidate.Type(), anchorInterface) {
					add("pointer_interface_implementation", "*"+candidate.Name(), typeName.Name(), candidate.Pos(), "go/types.Implements(pointer, selected interface)", "compile-time method-set predicate")
				}
			}
			continue
		}
		iface, ok := candidate.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		iface.Complete()
		if types.Implements(typeName.Type(), iface) {
			add("interface_obligation", typeName.Name(), candidate.Name(), typeName.Pos(), "go/types.Implements(value, interface)", "compile-time method-set predicate")
		}
		if types.Implements(types.NewPointer(typeName.Type()), iface) && !types.Implements(typeName.Type(), iface) {
			add("pointer_interface_obligation", "*"+typeName.Name(), candidate.Name(), typeName.Pos(), "go/types.Implements(pointer, interface)", "compile-time method-set predicate")
		}
	}
}

func addEmbedding(pkg typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	typeName, ok := anchor.(*types.TypeName)
	if !ok {
		return
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		return
	}
	structure, ok := named.Underlying().(*types.Struct)
	if ok {
		for i := 0; i < structure.NumFields(); i++ {
			field := structure.Field(i)
			if field.Embedded() {
				add("embedded_field", typeName.Name(), types.TypeString(field.Type(), qualifier), field.Pos(), "resolved embedded struct field", "promotion does not imply ownership")
			}
		}
	}
	iface, ok := named.Underlying().(*types.Interface)
	if ok {
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			embedded := iface.EmbeddedType(i)
			add("embedded_interface", typeName.Name(), types.TypeString(embedded, qualifier), typeName.Pos(), "resolved embedded interface", "type-set inclusion only")
		}
	}
	_ = pkg
}

func addLifecycleSites(pkg typedPackage, anchor types.Object, add func(string, string, string, token.Pos, string, ...string)) {
	anchorType := anchor.Type()
	for _, file := range pkg.files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			selection := pkg.info.Selections[selector]
			if selection == nil {
				return true
			}
			receiver := selection.Recv()
			if !sameNamedBase(receiver, anchorType) {
				return true
			}
			switch selector.Sel.Name {
			case "Close", "Stop", "Cancel":
				add("lifecycle_site", anchor.Name(), selector.Sel.Name, selector.Sel.Pos(), "resolved receiver type and lifecycle-shaped method call", "site only; does not prove ownership, guaranteed cancellation, or execution")
			}
			return true
		})
	}
}

func sameNamedBase(left, right types.Type) bool {
	for {
		pointer, ok := left.(*types.Pointer)
		if !ok {
			break
		}
		left = pointer.Elem()
	}
	for {
		pointer, ok := right.(*types.Pointer)
		if !ok {
			break
		}
		right = pointer.Elem()
	}
	return types.Identical(left, right)
}

func qualifier(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	return pkg.Path()
}

func typedLocation(pkg typedPackage, position token.Pos) Location {
	if !position.IsValid() {
		return Location{}
	}
	item := pkg.fset.PositionFor(position, false)
	file := pkg.relative[item.Filename]
	return Location{File: file, Line: item.Line, Column: item.Column, EndLine: item.Line, EndColumn: item.Column}
}
