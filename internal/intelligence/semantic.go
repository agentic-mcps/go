package intelligence

import "context"

// semanticReader is the deliberately small, neutral view used by the
// intelligence layer. LSP and gopls wire formats stop at this boundary.
type semanticReader interface {
	Search(context.Context, string) (semanticSymbols, error)
	SymbolAt(context.Context, string, Position) (SymbolMatch, error)
	Hover(context.Context, string, Position) (string, error)
	Definition(context.Context, string, Position) (semanticLocations, error)
	TypeDefinition(context.Context, string, Position) (semanticLocations, error)
	References(context.Context, string, Position) (semanticLocations, error)
	Implementations(context.Context, string, Position) (semanticSymbols, error)
	Diagnostics(context.Context, string) ([]Diagnostic, error)
	Calls(context.Context, string, Position) (semanticCalls, error)
}

type semanticLocations struct {
	Items   []Location
	Omitted int
}

type semanticSymbols struct {
	Items   []SymbolMatch
	Omitted int
}

type semanticCalls struct {
	Items   []CallEdge
	Omitted int
}

type semanticProvider interface {
	Read(context.Context, SnapshotRef, func(semanticReader) error) error
	Identity() SemanticIdentity
}
