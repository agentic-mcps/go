package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/gopls"
)

type rpcTextEdit struct {
	NewText string
	Range   rpcRange
}

type rpcWorkspaceEdit struct {
	Changes         map[string][]rpcTextEdit
	DocumentChanges []json.RawMessage
}

type rpcTextDocumentEdit struct {
	TextDocument struct{ URI string }
	Edits        []rpcTextEdit
}

type rpcCodeAction struct {
	Title   string
	Kind    string
	Edit    rpcWorkspaceEdit
	Command json.RawMessage
	Data    json.RawMessage
}

func (p *goplsProvider) Refactor(ctx context.Context, snapshot SnapshotRef, request semanticRefactorRequest) ([]semanticFileEdits, error) {
	var result []semanticFileEdits
	err := p.Read(ctx, snapshot, func(semanticReader) error {
		mutator := goplsMutator{p: p}
		var err error
		result, err = mutator.refactor(ctx, request)
		return err
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []semanticFileEdits{}
	}
	return result, nil
}

type goplsMutator struct {
	p *goplsProvider
}

func (m goplsMutator) refactor(ctx context.Context, request semanticRefactorRequest) ([]semanticFileEdits, error) {
	switch request.Operation {
	case RefactorRename:
		if strings.TrimSpace(request.File) == "" || strings.TrimSpace(request.NewName) == "" {
			return nil, fmt.Errorf("rename requires a symbol file and new name")
		}
		var edit rpcWorkspaceEdit
		params := map[string]any{
			"textDocument": textDocument(m.p.root, request.File),
			"position":     rpcPos(request.Position),
			"newName":      request.NewName,
		}
		if err := m.request(ctx, "textDocument/rename", params, &edit); err != nil {
			return nil, err
		}
		return m.normalizeWorkspaceEdit(edit)
	case RefactorFormat:
		return m.formatFiles(ctx, request.Files)
	case RefactorOrganizeImports:
		return m.codeActionFiles(ctx, request.Files, "source.organizeImports")
	case RefactorFixAll:
		return m.codeActionFiles(ctx, request.Files, "source.fixAll")
	default:
		return nil, fmt.Errorf("unsupported refactor operation %q", request.Operation)
	}
}

func (m goplsMutator) request(ctx context.Context, method string, params, out any) error {
	if err := m.p.manager.Request(ctx, method, params, out, false); err != nil {
		return fmt.Errorf("requesting guarded %s edits: %w", method, err)
	}
	return nil
}

func (m goplsMutator) formatFiles(ctx context.Context, files []string) ([]semanticFileEdits, error) {
	files, err := m.normalizeFiles(files)
	if err != nil {
		return nil, err
	}
	result := make([]semanticFileEdits, 0, len(files))
	for _, file := range files {
		var edits []rpcTextEdit
		params := map[string]any{
			"textDocument": textDocument(m.p.root, file),
			"options":      map[string]any{"tabSize": 8, "insertSpaces": false},
		}
		if err := m.request(ctx, "textDocument/formatting", params, &edits); err != nil {
			return nil, err
		}
		normalized, err := m.normalizeFileEdits(fileURI(m.p.root, file), edits)
		if err != nil {
			return nil, err
		}
		if len(normalized.Edits) > 0 {
			result = append(result, normalized)
		}
	}
	return result, nil
}

func (m goplsMutator) codeActionFiles(ctx context.Context, files []string, kind string) ([]semanticFileEdits, error) {
	files, err := m.normalizeFiles(files)
	if err != nil {
		return nil, err
	}
	workspaceEdit := rpcWorkspaceEdit{Changes: map[string][]rpcTextEdit{}}
	for _, file := range files {
		absolute, err := m.p.workspace.Resolve(file)
		if err != nil {
			return nil, fmt.Errorf("containing code action file %s: %w", file, err)
		}
		contents, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("reading code action file %s: %w", file, err)
		}
		end, err := gopls.PositionForOffset(contents, len(contents))
		if err != nil {
			return nil, fmt.Errorf("converting code action range for %s: %w", file, err)
		}
		params := map[string]any{
			"textDocument": textDocument(m.p.root, file),
			"range":        rpcRange{Start: rpcPos{}, End: rpcPos(end)},
			"context":      map[string]any{"diagnostics": []any{}, "only": []string{kind}},
		}
		var actions []rpcCodeAction
		if err := m.request(ctx, "textDocument/codeAction", params, &actions); err != nil {
			return nil, err
		}
		for _, action := range actions {
			if action.Kind != kind {
				return nil, fmt.Errorf("gopls returned unexpected code action kind %q", action.Kind)
			}
			if len(action.Command) > 0 && string(action.Command) != "null" {
				return nil, fmt.Errorf("code action %q requires an unsupported command", action.Title)
			}
			if emptyWorkspaceEdit(action.Edit) && len(action.Data) > 0 && string(action.Data) != "null" {
				resolved := action
				if err := m.request(ctx, "codeAction/resolve", action, &resolved); err != nil {
					return nil, err
				}
				action = resolved
			}
			if emptyWorkspaceEdit(action.Edit) {
				return nil, fmt.Errorf("code action %q did not provide source edits", action.Title)
			}
			for uri, edits := range action.Edit.Changes {
				workspaceEdit.Changes[uri] = append(workspaceEdit.Changes[uri], edits...)
			}
			workspaceEdit.DocumentChanges = append(workspaceEdit.DocumentChanges, action.Edit.DocumentChanges...)
		}
	}
	return m.normalizeWorkspaceEdit(workspaceEdit)
}

func emptyWorkspaceEdit(edit rpcWorkspaceEdit) bool {
	return len(edit.Changes) == 0 && len(edit.DocumentChanges) == 0
}

func (m goplsMutator) normalizeFiles(files []string) ([]string, error) {
	set := make(map[string]struct{}, len(files))
	for _, file := range files {
		relative, err := m.p.workspace.Relative(file)
		if err != nil {
			return nil, fmt.Errorf("containing refactor file %s: %w", file, err)
		}
		set[relative] = struct{}{}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("at least one refactor file is required")
	}
	result := make([]string, 0, len(set))
	for file := range set {
		result = append(result, file)
	}
	sort.Strings(result)
	return result, nil
}

func (m goplsMutator) normalizeWorkspaceEdit(edit rpcWorkspaceEdit) ([]semanticFileEdits, error) {
	byURI := make(map[string][]rpcTextEdit, len(edit.Changes))
	for uri, edits := range edit.Changes {
		byURI[uri] = append(byURI[uri], edits...)
	}
	for _, raw := range edit.DocumentChanges {
		var header struct{ Kind string }
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, fmt.Errorf("decoding workspace document change: %w", err)
		}
		if header.Kind != "" {
			return nil, fmt.Errorf("workspace resource operation %q is not allowed", header.Kind)
		}
		var document rpcTextDocumentEdit
		if err := json.Unmarshal(raw, &document); err != nil || document.TextDocument.URI == "" {
			return nil, fmt.Errorf("workspace document change is malformed")
		}
		byURI[document.TextDocument.URI] = append(byURI[document.TextDocument.URI], document.Edits...)
	}
	result := make([]semanticFileEdits, 0, len(byURI))
	for uri, edits := range byURI {
		normalized, err := m.normalizeFileEdits(uri, edits)
		if err != nil {
			return nil, err
		}
		if len(normalized.Edits) > 0 {
			result = append(result, normalized)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (m goplsMutator) normalizeFileEdits(uri string, edits []rpcTextEdit) (semanticFileEdits, error) {
	reader := goplsReader{p: m.p}
	file, ok := reader.file(uri)
	if !ok {
		return semanticFileEdits{}, fmt.Errorf("gopls edit target is outside the configured workspace")
	}
	absolute, err := m.p.workspace.Resolve(file)
	if err != nil {
		return semanticFileEdits{}, fmt.Errorf("containing refactor edit %s: %w", file, err)
	}
	contents, err := os.ReadFile(absolute)
	if err != nil {
		return semanticFileEdits{}, fmt.Errorf("reading refactor edit %s: %w", file, err)
	}
	normalized := make([]semanticTextEdit, 0, len(edits))
	for _, edit := range edits {
		start, err := gopls.OffsetForPosition(contents, gopls.Position(edit.Range.Start))
		if err != nil {
			return semanticFileEdits{}, fmt.Errorf("converting refactor start for %s: %w", file, err)
		}
		end, err := gopls.OffsetForPosition(contents, gopls.Position(edit.Range.End))
		if err != nil {
			return semanticFileEdits{}, fmt.Errorf("converting refactor end for %s: %w", file, err)
		}
		if end < start {
			return semanticFileEdits{}, fmt.Errorf("gopls returned an inverted edit for %s", file)
		}
		normalized = append(normalized, semanticTextEdit{Start: start, End: end, NewText: edit.NewText})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Start != normalized[j].Start {
			return normalized[i].Start < normalized[j].Start
		}
		if normalized[i].End != normalized[j].End {
			return normalized[i].End < normalized[j].End
		}
		return normalized[i].NewText < normalized[j].NewText
	})
	unique := normalized[:0]
	for _, edit := range normalized {
		if len(unique) > 0 {
			previous := unique[len(unique)-1]
			if edit == previous {
				continue
			}
			if edit.Start < previous.End || edit.Start == previous.Start {
				return semanticFileEdits{}, fmt.Errorf("gopls returned overlapping edits for %s", file)
			}
		}
		unique = append(unique, edit)
	}
	return semanticFileEdits{Path: file, Edits: unique}, nil
}
