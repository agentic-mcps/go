package changeimpact

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

func (a *Analyzer) computeImpact(ctx context.Context, analysis Analysis, options Options) (Analysis, error) {
	packages, err := a.loadPackages(ctx, options.Package)
	if err != nil {
		return Analysis{}, fmt.Errorf("loading active package scope: %w", err)
	}
	byID := make(map[string]packageRecord, len(packages))
	byDirectory := make(map[string][]string, len(packages))
	embeddedByFile := make(map[string][]string)
	modulePackages := make(map[string][]string)
	for _, pkg := range packages {
		byID[pkg.ImportPath] = pkg
		directory, relErr := a.workspace.Relative(pkg.Dir)
		if relErr != nil {
			return Analysis{}, fmt.Errorf("mapping package %q: %w", pkg.ImportPath, relErr)
		}
		byDirectory[directory] = append(byDirectory[directory], pkg.ImportPath)
		for _, file := range append(append(append([]string{}, pkg.EmbedFiles...), pkg.TestEmbedFiles...), pkg.XTestEmbedFiles...) {
			path, pathErr := a.packageFilePath(pkg, file)
			if pathErr != nil {
				return Analysis{}, pathErr
			}
			embeddedByFile[path] = append(embeddedByFile[path], pkg.ImportPath)
		}
		modulePackages[pkg.ModuleDir] = append(modulePackages[pkg.ModuleDir], pkg.ImportPath)
	}
	for directory := range byDirectory {
		sort.Strings(byDirectory[directory])
	}
	for path := range embeddedByFile {
		sort.Strings(embeddedByFile[path])
	}

	direct := make(map[string]map[string]struct{})
	addDirect := func(id, reason string) {
		if id == "" {
			return
		}
		if direct[id] == nil {
			direct[id] = make(map[string]struct{})
		}
		direct[id][reason] = struct{}{}
	}
	unmodelled := make([]verification.Uncertainty, 0)
	for _, changed := range analysis.Change.Files {
		mapped := false
		path := changed.Path
		if filepath.Ext(path) == ".go" {
			directory := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
			for _, id := range byDirectory[directory] {
				addDirect(id, "changed_source")
				mapped = true
			}
			if !mapped && changed.PreviousPath != "" {
				previousDirectory := filepath.ToSlash(filepath.Dir(filepath.FromSlash(changed.PreviousPath)))
				for _, id := range byDirectory[previousDirectory] {
					addDirect(id, "changed_source")
					mapped = true
				}
			}
			if !mapped {
				unmodelled = append(unmodelled, uncertaintyAt("inactive_go_file", "changed Go file is outside the active package graph for this build configuration", changed))
			}
		}
		for _, id := range embeddedByFile[path] {
			addDirect(id, "embedded_file")
			mapped = true
		}
		base := filepath.Base(filepath.FromSlash(path))
		switch {
		case path == "go.work" || path == "go.work.sum":
			for _, pkg := range packages {
				addDirect(pkg.ImportPath, "workspace_metadata")
			}
			mapped = true
		case base == "go.mod" || base == "go.sum":
			metadataDirectory := filepath.Dir(filepath.Join(a.workspace.Root(), filepath.FromSlash(path)))
			resolvedDirectory, resolveErr := filepath.EvalSymlinks(metadataDirectory)
			if resolveErr != nil {
				return Analysis{}, fmt.Errorf("resolving module metadata directory: %w", resolveErr)
			}
			for moduleDir, ids := range modulePackages {
				if moduleDir != resolvedDirectory {
					continue
				}
				for _, id := range ids {
					addDirect(id, "module_metadata")
					mapped = true
				}
			}
		}
		if !mapped && filepath.Ext(path) != ".go" {
			unmodelled = append(unmodelled, uncertaintyAt("unmodelled_non_go_change", "changed non-Go file is not an active embedded file or Go module/workspace manifest", changed))
		}
	}

	reverse := make(map[string][]string, len(packages))
	for _, pkg := range packages {
		imports := append(append(append([]string{}, pkg.Imports...), pkg.TestImports...), pkg.XTestImports...)
		seen := make(map[string]struct{}, len(imports))
		for _, imported := range imports {
			if _, selected := byID[imported]; !selected {
				continue
			}
			if _, duplicate := seen[imported]; duplicate {
				continue
			}
			seen[imported] = struct{}{}
			reverse[imported] = append(reverse[imported], pkg.ImportPath)
		}
	}
	for imported := range reverse {
		sort.Strings(reverse[imported])
	}

	distance := make(map[string]int, len(packages))
	reasons := make(map[string]map[string]struct{}, len(packages))
	queue := make([]string, 0, len(direct))
	for id, packageReasons := range direct {
		if _, active := byID[id]; !active {
			continue
		}
		distance[id] = 0
		reasons[id] = packageReasons
		queue = append(queue, id)
	}
	sort.Strings(queue)
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		for _, importer := range reverse[current] {
			if _, visited := distance[importer]; visited {
				continue
			}
			distance[importer] = distance[current] + 1
			reasons[importer] = map[string]struct{}{"reverse_import": {}}
			queue = append(queue, importer)
		}
	}

	ids := make([]string, 0, len(distance))
	for id := range distance {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if distance[ids[i]] != distance[ids[j]] {
			return distance[ids[i]] < distance[ids[j]]
		}
		return ids[i] < ids[j]
	})
	analysis.Impact.Packages = make([]verification.ImpactedPackage, 0, len(ids))
	analysis.Packages = make([]Package, 0, len(ids))
	for _, id := range ids {
		packageReasons := setValues(reasons[id])
		record := byID[id]
		analysis.Impact.Packages = append(analysis.Impact.Packages, verification.ImpactedPackage{
			Kind: "go.package", ID: id, Distance: distance[id], Reasons: packageReasons,
		})
		analysis.Packages = append(analysis.Packages, Package{
			ID: id, Dir: record.Dir, ModulePath: record.ModulePath, ModuleDir: record.ModuleDir,
			Distance: distance[id], Reasons: append([]string(nil), packageReasons...),
		})
	}
	analysis.ObservedPackages = len(ids)
	analysis.Uncertainties = append(analysis.Uncertainties, unmodelled...)
	if len(ids) > options.MaxPackages {
		analysis.Complete = false
		analysis.Uncertainties = append(analysis.Uncertainties, verification.Uncertainty{
			Code:      "package_limit",
			Message:   fmt.Sprintf("affected package closure contains %d packages, exceeding the configured limit of %d; narrow --package or raise --max-packages (maximum %d)", len(ids), options.MaxPackages, maximumMaxPackages),
			Locations: make([]verification.Location, 0),
		})
	}
	sort.Slice(analysis.Uncertainties, func(i, j int) bool {
		return analysis.Uncertainties[i].Code+"\x00"+analysis.Uncertainties[i].Message < analysis.Uncertainties[j].Code+"\x00"+analysis.Uncertainties[j].Message
	})
	return analysis, nil
}

func (a *Analyzer) packageFilePath(pkg packageRecord, path string) (string, error) {
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(pkg.Dir, filepath.FromSlash(path))
	}
	relative, err := a.workspace.Relative(absolute)
	if err != nil {
		return "", fmt.Errorf("mapping package %q file %q: %w", pkg.ImportPath, path, err)
	}
	return relative, nil
}

func uncertaintyAt(code, message string, changed verification.ChangedFile) verification.Uncertainty {
	line := 1
	if len(changed.CurrentRanges) > 0 {
		line = changed.CurrentRanges[0].Start
	} else if len(changed.BaseRanges) > 0 {
		line = changed.BaseRanges[0].Start
	}
	return verification.Uncertainty{
		Code: code, Message: message,
		Locations: []verification.Location{{File: changed.Path, Line: line}},
	}
}

func setValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
