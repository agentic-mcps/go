package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/gopls"
)

type fakeGoplsRPC struct {
	responses map[string]json.RawMessage
	notifies  []goplsNotification
	requests  []goplsRequest
	restarts  int
	caps      gopls.Capabilities
}

type goplsRequest struct {
	params     any
	method     string
	idempotent bool
}

type goplsNotification struct {
	params any
	method string
}

func (f *fakeGoplsRPC) Request(_ context.Context, method string, params any, out any, idempotent bool) error {
	f.requests = append(f.requests, goplsRequest{method: method, params: params, idempotent: idempotent})
	response, ok := f.responses[method]
	if !ok {
		return errors.New("unexpected request: " + method)
	}
	return json.Unmarshal(response, out)
}

func TestGoplsMutatorNormalizesUTF16RenameWithoutMutationRetry(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	writeSnapshotFile(t, root, "unicode.go", "package fixture\n\nvar πValue = 1\n")
	rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{
		"textDocument/rename": json.RawMessage(`{"changes":{"` + fileURI(snapshots.workspace.Root(), "unicode.go") + `":[{"range":{"start":{"line":2,"character":4},"end":{"line":2,"character":10}},"newText":"πRenamed"}]}}`),
	}}
	provider, err := newGoplsProvider(rpc, snapshots.workspace, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := provider.Refactor(context.Background(), snapshot, semanticRefactorRequest{
		Operation: RefactorRename, File: "unicode.go", Position: Position{Line: 2, Character: 4}, NewName: "πRenamed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].Path != "unicode.go" || len(edits[0].Edits) != 1 || edits[0].Edits[0].Start != 21 || edits[0].Edits[0].End != 28 {
		t.Fatalf("normalized edits = %#v", edits)
	}
	if len(rpc.requests) != 1 || rpc.requests[0].method != "textDocument/rename" || rpc.requests[0].idempotent {
		t.Fatalf("requests = %#v", rpc.requests)
	}
	encoded, err := json.Marshal(rpc.requests[0].params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"newName":"πRenamed"`) {
		t.Fatalf("rename params = %s", encoded)
	}
}

func TestGoplsMutatorRejectsOverlapsAndResourceOperations(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	writeSnapshotFile(t, root, "value.go", "package fixture\n\nvar Value = 1\n")
	for name, response := range map[string]string{
		"overlap":  `{"changes":{"` + fileURI(snapshots.workspace.Root(), "value.go") + `":[{"range":{"start":{"line":2,"character":4},"end":{"line":2,"character":9}},"newText":"First"},{"range":{"start":{"line":2,"character":5},"end":{"line":2,"character":9}},"newText":"Second"}]}}`,
		"resource": `{"documentChanges":[{"kind":"create","uri":"` + fileURI(snapshots.workspace.Root(), "created.go") + `"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{"textDocument/rename": json.RawMessage(response)}}
			provider, err := newGoplsProvider(rpc, snapshots.workspace, snapshots)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Refactor(context.Background(), snapshot, semanticRefactorRequest{Operation: RefactorRename, File: "value.go", NewName: "Changed"})
			if err == nil {
				t.Fatal("Refactor() accepted unsafe workspace edit")
			}
		})
	}
}

func TestGoplsMutatorRequestsFormattingAndAllowedCodeActions(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	writeSnapshotFile(t, root, "value.go", "package fixture\n\nvar Value=1\n")
	for operation, testCase := range map[string]struct{ method, response string }{
		RefactorFormat:          {"textDocument/formatting", `[{"range":{"start":{"line":2,"character":9},"end":{"line":2,"character":9}},"newText":" "}]`},
		RefactorOrganizeImports: {"textDocument/codeAction", `[{"title":"Organize Imports","kind":"source.organizeImports","edit":{"changes":{"` + fileURI(snapshots.workspace.Root(), "value.go") + `":[{"range":{"start":{"line":2,"character":9},"end":{"line":2,"character":9}},"newText":" "}]}}}]`},
		RefactorFixAll:          {"textDocument/codeAction", `[{"title":"Fix all","kind":"source.fixAll","edit":{"changes":{"` + fileURI(snapshots.workspace.Root(), "value.go") + `":[{"range":{"start":{"line":2,"character":9},"end":{"line":2,"character":9}},"newText":" "}]}}}]`},
	} {
		t.Run(operation, func(t *testing.T) {
			rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{testCase.method: json.RawMessage(testCase.response)}}
			provider, err := newGoplsProvider(rpc, snapshots.workspace, snapshots)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
			if err != nil {
				t.Fatal(err)
			}
			edits, err := provider.Refactor(context.Background(), snapshot, semanticRefactorRequest{Operation: operation, Files: []string{"value.go"}})
			if err != nil || len(edits) != 1 || len(edits[0].Edits) != 1 {
				t.Fatalf("edits=%#v error=%v", edits, err)
			}
			if len(rpc.requests) != 1 || rpc.requests[0].method != testCase.method || rpc.requests[0].idempotent {
				t.Fatalf("requests = %#v", rpc.requests)
			}
			if testCase.method == "textDocument/codeAction" {
				encoded, marshalErr := json.Marshal(rpc.requests[0].params)
				if marshalErr != nil || !strings.Contains(string(encoded), `"only":["source.`) {
					t.Fatalf("code action params = %s, %v", encoded, marshalErr)
				}
			}
		})
	}
}

func (f *fakeGoplsRPC) Notify(method string, params any) error {
	f.notifies = append(f.notifies, goplsNotification{method: method, params: params})
	return nil
}

func (f *fakeGoplsRPC) Restart(context.Context) error             { f.restarts++; return nil }
func (f *fakeGoplsRPC) Capabilities() (gopls.Capabilities, error) { return f.caps, nil }

func TestGoplsProviderSynchronizesChangedFilesAtRequestBoundaries(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{}}
	provider, err := newGoplsProvider(rpc, snapshots.workspace, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	first, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	if readErr := provider.Read(context.Background(), first, func(semanticReader) error { return nil }); readErr != nil {
		t.Fatal(readErr)
	}
	if len(rpc.notifies) != 0 {
		t.Fatalf("initial notifications = %d, want 0", len(rpc.notifies))
	}

	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 22\n")
	second, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	if readErr := provider.Read(context.Background(), second, func(semanticReader) error { return nil }); readErr != nil {
		t.Fatal(readErr)
	}
	if len(rpc.notifies) != 1 || rpc.notifies[0].method != "workspace/didChangeWatchedFiles" {
		t.Fatalf("notifications = %#v", rpc.notifies)
	}
	encoded, err := json.Marshal(rpc.notifies[0].params)
	if err != nil {
		t.Fatal(err)
	}
	var notification struct {
		Changes []struct {
			URI  string `json:"uri"`
			Type int    `json:"type"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(encoded, &notification); err != nil {
		t.Fatal(err)
	}
	if len(notification.Changes) != 1 || notification.Changes[0].Type != 2 || notification.Changes[0].URI != fileURI(provider.root, "main.go") {
		t.Fatalf("changes = %#v", notification.Changes)
	}
}

func TestGoplsProviderRestartsForConfigurationAndDoesNotAdvanceOnFailure(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{}}
	provider, err := newGoplsProvider(rpc, snapshots.workspace, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	first, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	if readErr := provider.Read(context.Background(), first, func(semanticReader) error { return nil }); readErr != nil {
		t.Fatal(readErr)
	}
	writeSnapshotFile(t, root, "go.mod", "module example.test/snapshot\n\ngo 1.25.0\n\n// restart\n")
	second, err := snapshots.Capture(context.Background(), SnapshotRequest{Semantic: provider.Identity()})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("reader failed")
	if err := provider.Read(context.Background(), second, func(semanticReader) error { return want }); !errors.Is(err, want) {
		t.Fatalf("Read() error = %v", err)
	}
	if rpc.restarts != 1 || provider.last.ID != first.ID {
		t.Fatalf("restarts=%d last=%q, want 1 and %q", rpc.restarts, provider.last.ID, first.ID)
	}
}

func TestGoplsReaderNormalizesSymbolsLocationsAndDiagnostics(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	root = snapshots.workspace.Root()
	if err := os.WriteFile(filepath.Join(root, "value.go"), []byte("package sample\n\nfunc Value() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rpc := &fakeGoplsRPC{responses: map[string]json.RawMessage{
		"workspace/symbol":            json.RawMessage(`[{"name":"Value","kind":12,"containerName":"example.test/sample","location":{"uri":"` + fileURI(root, "value.go") + `","range":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}},{"name":"Println","kind":12,"location":{"uri":"file:///outside/fmt.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}}]`),
		"textDocument/documentSymbol": json.RawMessage(`[{"name":"Value","detail":"func()","kind":12,"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":15}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}]`),
		"textDocument/definition":     json.RawMessage(`{"uri":"` + fileURI(root, "value.go") + `","range":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}`),
		"textDocument/typeDefinition": json.RawMessage(`[{"targetUri":"` + fileURI(root, "value.go") + `","targetSelectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}]`),
		"textDocument/references":     json.RawMessage(`[{"uri":"file:///outside/value.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]`),
		"textDocument/diagnostic":     json.RawMessage(`{"kind":"full","items":[{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":5}},"severity":2,"code":"unused","source":"compiler","message":"example"}]}`),
	}}
	reader := &goplsReader{p: &goplsProvider{manager: rpc, root: root, workspace: snapshots.workspace}, snapshot: SnapshotRef{ID: "snapshot", Scope: "./..."}}
	search, err := reader.Search(context.Background(), "Value")
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Items) != 1 || search.Omitted != 1 || search.Items[0].Kind != "go.function" || search.Items[0].Package != "example.test/sample" {
		t.Fatalf("search = %#v", search)
	}
	symbol, err := reader.SymbolAt(context.Background(), "value.go", Position{Line: 2, Character: 7})
	if err != nil {
		t.Fatal(err)
	}
	if symbol.Name != "Value" || symbol.Location.Column != 6 {
		t.Fatalf("symbol = %#v", symbol)
	}
	definition, err := reader.Definition(context.Background(), "value.go", Position{Line: 2, Character: 7})
	if err != nil || len(definition.Items) != 1 {
		t.Fatalf("definition=%#v error=%v", definition, err)
	}
	typeDefinition, err := reader.TypeDefinition(context.Background(), "value.go", Position{Line: 2, Character: 7})
	if err != nil || len(typeDefinition.Items) != 1 {
		t.Fatalf("type definition=%#v error=%v", typeDefinition, err)
	}
	references, err := reader.References(context.Background(), "value.go", Position{Line: 2, Character: 7})
	if err != nil || references.Omitted != 1 || len(references.Items) != 0 {
		t.Fatalf("references=%#v error=%v", references, err)
	}
	diagnostics, err := reader.Diagnostics(context.Background(), "value.go")
	if err != nil {
		t.Fatal(err)
	}
	wantDiagnostic := Diagnostic{Source: "compiler", Code: "unused", Severity: "warning", Message: "example", Location: Location{File: "value.go", Line: 3, Column: 1, EndLine: 3, EndColumn: 6}}
	if !reflect.DeepEqual(diagnostics, []Diagnostic{wantDiagnostic}) {
		t.Fatalf("diagnostics=%#v want %#v", diagnostics, wantDiagnostic)
	}
}

func TestGoplsReaderConvertsUTF16RangesToUTF8ByteColumns(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	root = snapshots.workspace.Root()
	if err := os.WriteFile(filepath.Join(root, "unicode.go"), []byte("package sample\n\nvar πValue = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := &goplsReader{p: &goplsProvider{root: root, workspace: snapshots.workspace}}
	location, err := reader.sourceLocation("unicode.go", rpcRange{
		Start: rpcPos{Line: 2, Character: 4},
		End:   rpcPos{Line: 2, Character: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if location.Column != 5 || location.EndColumn != 12 {
		t.Fatalf("location = %#v, want UTF-8 byte columns 5..12", location)
	}
}

func TestGoplsReaderRejectsSymlinkedLocationOutsideWorkspace(t *testing.T) {
	root := snapshotRepository(t)
	snapshots := newTestSnapshotter(t, root)
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(snapshots.workspace.Root(), "escape.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	reader := &goplsReader{p: &goplsProvider{root: snapshots.workspace.Root(), workspace: snapshots.workspace}}
	if file, ok := reader.file(fileURI(snapshots.workspace.Root(), "escape.go")); ok {
		t.Fatalf("outside symlink accepted as %q", file)
	}
}
