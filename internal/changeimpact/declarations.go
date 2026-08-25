package changeimpact

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"golang.org/x/mod/modfile"
)

type sourceDeclaration struct {
	kind     string
	name     string
	start    int
	end      int
	location verification.Location
}

func (a *Analyzer) changedDeclarations(files []File) ([]verification.ChangedDeclaration, error) {
	changed := make([]verification.ChangedDeclaration, 0)
	for _, file := range files {
		if filepath.Ext(file.Change.Path) != ".go" {
			continue
		}
		packageID, err := a.packageID(file.Change.Path, file.Change.PreviousPath)
		if err != nil {
			return nil, fmt.Errorf("mapping declarations in %q: %w", file.Change.Path, err)
		}
		base, err := parseSourceDeclarations(file.BaseContent, previousOrCurrent(file.Change), packageID)
		if err != nil {
			return nil, fmt.Errorf("parsing base declarations in %q: %w", file.Change.Path, err)
		}
		current, err := parseSourceDeclarations(file.CurrentContent, file.Change.Path, packageID)
		if err != nil {
			return nil, fmt.Errorf("parsing current declarations in %q: %w", file.Change.Path, err)
		}
		keys := make(map[string]struct{}, len(base)+len(current))
		for key := range base {
			keys[key] = struct{}{}
		}
		for key := range current {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			old, hasOld := base[key]
			now, hasCurrent := current[key]
			if hasOld && !intersects(old.start, old.end, file.Change.BaseRanges) && hasCurrent && !intersects(now.start, now.end, file.Change.CurrentRanges) {
				continue
			}
			if hasOld && !hasCurrent && !intersects(old.start, old.end, file.Change.BaseRanges) {
				continue
			}
			if hasCurrent && !hasOld && !intersects(now.start, now.end, file.Change.CurrentRanges) {
				continue
			}
			item := verification.ChangedDeclaration{Package: packageID}
			switch {
			case hasOld && hasCurrent:
				item.Kind = now.kind
				item.Name = now.name
				item.Change = verification.ChangeModified
				item.BaseLocation = &old.location
				item.CurrentLocation = &now.location
			case hasOld:
				item.Kind = old.kind
				item.Name = old.name
				item.Change = verification.ChangeDeleted
				item.BaseLocation = &old.location
			case hasCurrent:
				item.Kind = now.kind
				item.Name = now.name
				item.Change = verification.ChangeAdded
				if file.Change.Change == verification.ChangeUntracked {
					item.Change = verification.ChangeUntracked
				}
				item.CurrentLocation = &now.location
			}
			changed = append(changed, item)
		}
	}
	sort.Slice(changed, func(i, j int) bool {
		left := changed[i].Package + "\x00" + locationFile(changed[i]) + fmt.Sprintf("\x00%09d", locationLine(changed[i])) + changed[i].Name
		right := changed[j].Package + "\x00" + locationFile(changed[j]) + fmt.Sprintf("\x00%09d", locationLine(changed[j])) + changed[j].Name
		return left < right
	})
	return changed, nil
}

func parseSourceDeclarations(content []byte, path, packageID string) (map[string]sourceDeclaration, error) {
	result := make(map[string]sourceDeclaration)
	if len(content) == 0 {
		return result, nil
	}
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, path, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	add := func(kind, name string, node ast.Node) {
		start := files.Position(node.Pos())
		end := files.Position(node.End())
		declaration := sourceDeclaration{
			kind:  kind,
			name:  name,
			start: start.Line,
			end:   end.Line,
			location: verification.Location{
				File: path,
				Line: start.Line,
				Col:  start.Column,
			},
		}
		result[kind+"\x00"+name] = declaration
	}
	for _, declaration := range parsed.Decls {
		switch item := declaration.(type) {
		case *ast.FuncDecl:
			kind := "function"
			name := item.Name.Name
			if item.Recv != nil && len(item.Recv.List) > 0 {
				kind = "method"
				name = receiverName(item.Recv.List[0].Type) + "." + name
			}
			add(kind, name, item)
		case *ast.GenDecl:
			for _, specification := range item.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					add("type", spec.Name.Name, spec)
					if structure, ok := spec.Type.(*ast.StructType); ok {
						for _, field := range structure.Fields.List {
							for _, name := range field.Names {
								add("field", spec.Name.Name+"."+name.Name, field)
							}
						}
					}
				case *ast.ValueSpec:
					kind := "variable"
					if item.Tok == token.CONST {
						kind = "constant"
					}
					for _, name := range spec.Names {
						add(kind, name.Name, spec)
					}
				}
			}
		}
	}
	_ = packageID
	return result, nil
}

func (a *Analyzer) packageID(path, previous string) (string, error) {
	candidate := path
	if _, err := os.Stat(filepath.Join(a.workspace.Root(), filepath.FromSlash(candidate))); err != nil && previous != "" {
		candidate = previous
	}
	directory := filepath.Dir(filepath.Join(a.workspace.Root(), filepath.FromSlash(candidate)))
	for {
		moduleFile := filepath.Join(directory, "go.mod")
		data, err := os.ReadFile(moduleFile)
		if err == nil {
			modulePath := modfile.ModulePath(data)
			if modulePath == "" {
				return "", fmt.Errorf("module path missing from %s", moduleFile)
			}
			relative, relErr := filepath.Rel(directory, filepath.Dir(filepath.Join(a.workspace.Root(), filepath.FromSlash(candidate))))
			if relErr != nil {
				return "", relErr
			}
			if relative == "." {
				return modulePath, nil
			}
			return strings.TrimSuffix(modulePath, "/") + "/" + filepath.ToSlash(relative), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if directory == a.workspace.Root() {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory || !pathWithin(a.workspace.Root(), parent) {
			break
		}
		directory = parent
	}
	return "", fmt.Errorf("no containing go.mod")
}

func receiverName(expression ast.Expr) string {
	switch item := expression.(type) {
	case *ast.Ident:
		return item.Name
	case *ast.StarExpr:
		return receiverName(item.X)
	case *ast.IndexExpr:
		return receiverName(item.X)
	case *ast.IndexListExpr:
		return receiverName(item.X)
	default:
		return "receiver"
	}
}

func previousOrCurrent(change verification.ChangedFile) string {
	if change.PreviousPath != "" {
		return change.PreviousPath
	}
	return change.Path
}

func intersects(start, end int, ranges []verification.LineRange) bool {
	for _, changed := range ranges {
		if start <= changed.End && end >= changed.Start {
			return true
		}
	}
	return false
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func locationFile(item verification.ChangedDeclaration) string {
	if item.CurrentLocation != nil {
		return item.CurrentLocation.File
	}
	return item.BaseLocation.File
}

func locationLine(item verification.ChangedDeclaration) int {
	if item.CurrentLocation != nil {
		return item.CurrentLocation.Line
	}
	return item.BaseLocation.Line
}
