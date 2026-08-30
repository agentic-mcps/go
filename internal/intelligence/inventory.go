package intelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/workspace"
)

// inventoryPackage is deliberately private: it is the source adapter for a
// ContextPack, not another public protocol.
type inventoryPackage struct {
	ImportPath                        string
	Dir                               string
	Name                              string
	ModulePath                        string
	ModuleDir                         string
	ModuleGo                          string
	GoFiles, CgoFiles, IgnoredGoFiles []string
	TestGoFiles, XTestGoFiles         []string
	Imports                           []string
}

type inventoryList struct {
	Module *struct {
		Path      string `json:"Path"`
		Dir       string `json:"Dir"`
		GoVersion string `json:"GoVersion"`
	} `json:"Module"`
	Error *struct {
		Err string `json:"Err"`
	} `json:"Error"`
	ImportPath     string   `json:"ImportPath"`
	Dir            string   `json:"Dir"`
	Name           string   `json:"Name"`
	GoFiles        []string `json:"GoFiles"`
	CgoFiles       []string `json:"CgoFiles"`
	IgnoredGoFiles []string `json:"IgnoredGoFiles"`
	TestGoFiles    []string `json:"TestGoFiles"`
	XTestGoFiles   []string `json:"XTestGoFiles"`
	Imports        []string `json:"Imports"`
}

func inventoryPackages(ctx context.Context, ws *workspace.Workspace, runner *execution.Runner, scope string) ([]inventoryPackage, error) {
	if strings.TrimSpace(scope) == "" {
		scope = "./..."
	}
	var out, errOut bytes.Buffer
	result, err := runner.Run(ctx, execution.Command{Name: ws.Toolchain().Path, Args: []string{"list", "-json", "-mod=readonly", scope}, Env: map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "auto"}}, execution.Streams{Stdout: &out, Stderr: &errOut})
	if err != nil {
		return nil, fmt.Errorf("listing workspace packages: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("listing workspace packages: %s", strings.TrimSpace(errOut.String()))
	}
	decoder := json.NewDecoder(&out)
	packages := make([]inventoryPackage, 0)
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var item inventoryList
		if err := decoder.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding package inventory: %w", err)
		}
		if item.Error != nil {
			return nil, fmt.Errorf("package %s: %s", item.ImportPath, item.Error.Err)
		}
		if item.ImportPath == "" || item.Dir == "" || seen[item.ImportPath] {
			continue
		}
		if _, err := ws.Relative(item.Dir); err != nil {
			continue
		}
		p := inventoryPackage{ImportPath: item.ImportPath, Dir: item.Dir, Name: item.Name, GoFiles: item.GoFiles, CgoFiles: item.CgoFiles, IgnoredGoFiles: item.IgnoredGoFiles, TestGoFiles: item.TestGoFiles, XTestGoFiles: item.XTestGoFiles, Imports: item.Imports}
		if item.Module != nil {
			p.ModulePath, p.ModuleDir, p.ModuleGo = item.Module.Path, item.Module.Dir, item.Module.GoVersion
		}
		packages = append(packages, p)
		seen[item.ImportPath] = true
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	return packages, nil
}

func summarizeInventory(ctx context.Context, ws *workspace.Workspace, packages []inventoryPackage) ([]PackageSummary, []ModuleSummary, error) {
	result := make([]PackageSummary, 0, len(packages))
	modules := make(map[string]ModuleSummary)
	for _, p := range packages {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		rel, err := ws.Relative(p.Dir)
		if err != nil {
			return nil, nil, err
		}
		exported, generated, constrained, err := exportedInventory(ctx, p.Dir, append(append([]string{}, p.GoFiles...), p.CgoFiles...))
		if err != nil {
			return nil, nil, err
		}
		result = append(result, PackageSummary{Kind: "go.package", ID: p.ImportPath, Name: p.Name, Directory: rel, Module: p.ModulePath, Imports: len(p.Imports), Tests: len(p.TestGoFiles) + len(p.XTestGoFiles), Exported: exported, Cgo: len(p.CgoFiles) > 0, Generated: generated, Constrained: constrained || len(p.IgnoredGoFiles) > 0})
		if p.ModulePath != "" {
			moduleWorkspace, relativeErr := ws.Relative(p.ModuleDir)
			if relativeErr != nil {
				return nil, nil, relativeErr
			}
			modules[p.ModulePath] = ModuleSummary{Path: p.ModulePath, GoVersion: p.ModuleGo, Workspace: moduleWorkspace}
		}
	}
	ms := make([]ModuleSummary, 0, len(modules))
	for _, m := range modules {
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Path < ms[j].Path })
	return result, ms, nil
}

func exportedInventory(ctx context.Context, dir string, files []string) ([]string, bool, bool, error) {
	set := map[string]bool{}
	generated, constrained := false, false
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false, false, err
		}
		if bytes.Contains(data, []byte("Code generated ")) {
			generated = true
		}
		if bytes.Contains(data, []byte("//go:build")) || bytes.Contains(data, []byte("// +build")) {
			constrained = true
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ParseComments)
		if err != nil {
			return nil, false, false, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Name.IsExported() {
					signature := *decl
					signature.Doc = nil
					signature.Body = nil
					formatted, formatErr := formatInventoryNode(&signature)
					if formatErr != nil {
						return nil, false, false, fmt.Errorf("formatting %s: %w", decl.Name.Name, formatErr)
					}
					set[formatted] = true
				}
			case *ast.GenDecl:
				for _, s := range decl.Specs {
					switch x := s.(type) {
					case *ast.TypeSpec:
						if x.Name.IsExported() {
							formatted, formatErr := formatInventoryNode(&ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{x}})
							if formatErr != nil {
								return nil, false, false, fmt.Errorf("formatting %s: %w", x.Name.Name, formatErr)
							}
							set[formatted] = true
						}
					case *ast.ValueSpec:
						for _, n := range x.Names {
							if n.IsExported() {
								valueSpec := &ast.ValueSpec{Names: []*ast.Ident{n}, Type: x.Type}
								formatted, formatErr := formatInventoryNode(&ast.GenDecl{Tok: decl.Tok, Specs: []ast.Spec{valueSpec}})
								if formatErr != nil {
									return nil, false, false, fmt.Errorf("formatting %s: %w", n.Name, formatErr)
								}
								set[formatted] = true
							}
						}
					}
				}
			}
		}
	}
	result := make([]string, 0, len(set))
	for s := range set {
		result = append(result, s)
	}
	sort.Strings(result)
	return result, generated, constrained, nil
}

func formatInventoryNode(node any) (string, error) {
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}

func inventoryGuidance(ctx context.Context, ws *workspace.Workspace) ([]GuidanceRef, error) {
	refs := make([]GuidanceRef, 0)
	err := filepath.WalkDir(ws.Root(), func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != ws.Root() && (d.Name() == ".git" || d.Name() == "vendor") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "AGENTS.md" && d.Name() != "CLAUDE.md" {
			return nil
		}
		rel, e := ws.Relative(path)
		if e != nil {
			return e
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(data)
		refs = append(refs, GuidanceRef{File: rel, Digest: hex.EncodeToString(sum[:])})
		return nil
	})
	sort.Slice(refs, func(i, j int) bool { return refs[i].File < refs[j].File })
	return refs, err
}
