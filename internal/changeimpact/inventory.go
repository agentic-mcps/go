package changeimpact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"golang.org/x/mod/modfile"
)

// packageRecord is the bounded package inventory used by impact analysis.
// Paths are absolute and symlink-resolved; file names are workspace-relative
// only after the later source-mapping stage.
type packageRecord struct {
	ImportPath string
	Dir        string
	ModulePath string
	ModuleDir  string

	GoFiles         []string
	CgoFiles        []string
	IgnoredGoFiles  []string
	TestGoFiles     []string
	XTestGoFiles    []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Imports         []string
	TestImports     []string
	XTestImports    []string
}

type goListPackage struct {
	ImportPath      string
	Dir             string
	GoFiles         []string
	CgoFiles        []string
	IgnoredGoFiles  []string
	TestGoFiles     []string
	XTestGoFiles    []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Imports         []string
	TestImports     []string
	XTestImports    []string
	Error           *struct{ Err string }
	Module          *struct {
		Path string
		Dir  string
	}
}

func (a *Analyzer) loadPackages(ctx context.Context, pattern string) ([]packageRecord, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("package pattern is empty")
	}
	callCtx, cancel := a.runner.Deadline(ctx)
	defer cancel()

	type invocation struct {
		dir     string
		pattern string
	}
	invocations := []invocation{{dir: a.workspace.Root(), pattern: pattern}}
	if pattern == defaultPackagePattern {
		if workPath, ok := containedWorkFile(a.workspace.Root()); ok {
			invocations = nil
			uses, err := workModuleUses(workPath)
			if err != nil {
				return nil, err
			}
			for _, use := range uses {
				invocations = append(invocations, invocation{dir: use, pattern: defaultPackagePattern})
			}
		}
	}

	byImport := make(map[string]packageRecord)
	for _, call := range invocations {
		var stdout, stderr bytes.Buffer
		result, err := a.runner.Run(callCtx, execution.Command{
			Name: "go", Dir: call.dir,
			Args: []string{"list", "-json", "-mod=readonly", call.pattern},
			Env:  map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "auto"},
		}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
		if err != nil {
			return nil, fmt.Errorf("listing packages in %s: %w", call.dir, err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("listing packages in %s: go list exited %d: %s", call.dir, result.ExitCode, boundedInventoryMessage(stderr.String()))
		}
		records, err := decodePackageRecords(&stdout)
		if err != nil {
			return nil, fmt.Errorf("decoding package inventory in %s: %w", call.dir, err)
		}
		for _, record := range records {
			normalized, err := a.normalizePackageRecord(record)
			if err != nil {
				return nil, err
			}
			if existing, ok := byImport[normalized.ImportPath]; !ok || normalized.Dir < existing.Dir {
				byImport[normalized.ImportPath] = normalized
			}
		}
	}

	resultRecords := make([]packageRecord, 0, len(byImport))
	for _, record := range byImport {
		resultRecords = append(resultRecords, record)
	}
	sort.Slice(resultRecords, func(i, j int) bool {
		if resultRecords[i].ImportPath != resultRecords[j].ImportPath {
			return resultRecords[i].ImportPath < resultRecords[j].ImportPath
		}
		return resultRecords[i].Dir < resultRecords[j].Dir
	})
	return resultRecords, nil
}

func decodePackageRecords(r io.Reader) ([]packageRecord, error) {
	decoder := json.NewDecoder(r)
	result := make([]packageRecord, 0)
	for {
		var listed goListPackage
		err := decoder.Decode(&listed)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if listed.Error != nil && listed.Error.Err != "" {
			return nil, fmt.Errorf("package %q: %s", listed.ImportPath, listed.Error.Err)
		}
		if listed.ImportPath == "" || listed.Dir == "" {
			return nil, fmt.Errorf("go list returned incomplete package metadata")
		}
		modulePath, moduleDir := "", ""
		if listed.Module != nil {
			modulePath, moduleDir = listed.Module.Path, listed.Module.Dir
		}
		result = append(result, packageRecord{
			ImportPath: listed.ImportPath, Dir: listed.Dir, ModulePath: modulePath, ModuleDir: moduleDir,
			GoFiles: listed.GoFiles, CgoFiles: listed.CgoFiles, IgnoredGoFiles: listed.IgnoredGoFiles,
			TestGoFiles: listed.TestGoFiles, XTestGoFiles: listed.XTestGoFiles,
			EmbedFiles: listed.EmbedFiles, TestEmbedFiles: listed.TestEmbedFiles, XTestEmbedFiles: listed.XTestEmbedFiles,
			Imports: listed.Imports, TestImports: listed.TestImports, XTestImports: listed.XTestImports,
		})
	}
	return result, nil
}

func (a *Analyzer) normalizePackageRecord(record packageRecord) (packageRecord, error) {
	if record.ImportPath == "" || record.Dir == "" {
		return packageRecord{}, fmt.Errorf("package inventory contains incomplete metadata")
	}
	dir, err := a.workspace.Resolve(record.Dir)
	if err != nil {
		return packageRecord{}, fmt.Errorf("package %q is outside the configured workspace: %w", record.ImportPath, err)
	}
	record.Dir = dir
	if record.ModuleDir != "" {
		record.ModuleDir, err = a.workspace.Resolve(record.ModuleDir)
		if err != nil {
			return packageRecord{}, fmt.Errorf("module for package %q is outside the configured workspace: %w", record.ImportPath, err)
		}
	}
	return record, nil
}

func containedWorkFile(root string) (string, bool) {
	path := filepath.Join(root, "go.work")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

func workModuleUses(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	work, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	uses := make([]string, 0, len(work.Use))
	for _, use := range work.Use {
		module := use.Path
		if !filepath.IsAbs(module) {
			module = filepath.Join(filepath.Dir(path), module)
		}
		uses = append(uses, filepath.Clean(module))
	}
	sort.Strings(uses)
	return uses, nil
}

func boundedInventoryMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 512 {
		return value
	}
	return value[:512]
}
