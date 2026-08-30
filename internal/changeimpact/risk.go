package changeimpact

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/agentic-mcps/go/internal/verification"
)

type riskDefinition struct {
	reason   string
	guidance string
}

var riskDefinitions = map[string]riskDefinition{
	"exported_api_change": {
		reason:   "Exported declarations changed, which can affect callers and public contracts.",
		guidance: "Review API compatibility, naming, documentation, and maintainability at the changed declarations.",
	},
	"module_graph_change": {
		reason:   "Go module or workspace metadata changed.",
		guidance: "Review dependency provenance, version movement, checksum changes, replacement directives, and supply-chain implications.",
	},
	"synchronization_change": {
		reason:   "Changed source contains a goroutine, channel, select, or synchronization operation.",
		guidance: "Review ownership, ordering, cancellation, lifecycle, and race behavior; consider the optional race check.",
	},
	"error_flow_change": {
		reason:   "Changed source alters error checks, error construction, recovery, or process termination flow.",
		guidance: "Review propagation, wrapping, cleanup, caller observability, and termination behavior.",
	},
	"http_boundary_change": {
		reason:   "Changed source touches an HTTP boundary.",
		guidance: "Review authentication, authorization, validation, timeouts, body limits, response semantics, and logging.",
	},
	"database_boundary_change": {
		reason:   "Changed source touches a database boundary.",
		guidance: "Review transaction boundaries, query parameterization, cancellation, retries, connection use, and error handling.",
	},
	"crypto_boundary_change": {
		reason:   "Changed source touches cryptographic operations.",
		guidance: "Review primitive choice, key and nonce handling, randomness, secret lifetime, and failure behavior.",
	},
	"serialization_boundary_change": {
		reason:   "Changed source touches serialization or wire-format handling.",
		guidance: "Review compatibility, validation, unknown fields, resource bounds, and untrusted-input behavior.",
	},
	"observability_boundary_change": {
		reason:   "Changed source touches logging, tracing, or metrics instrumentation.",
		guidance: "Review signal usefulness, cardinality, sensitive data, correlation, error visibility, and failure-path coverage.",
	},
	"performance_sensitive_change": {
		reason:   "Changed source touches loops, allocation-capable operations, reflection, formatting, or benchmark code.",
		guidance: "Review hot-path cost and allocation behavior; benchmark only when the changed path is performance-sensitive.",
	},
}

type riskCollector struct {
	locations map[string]map[string]verification.Location
}

func newRiskCollector() *riskCollector {
	return &riskCollector{locations: make(map[string]map[string]verification.Location)}
}

func (c *riskCollector) add(code string, location verification.Location) {
	if c.locations[code] == nil {
		c.locations[code] = make(map[string]verification.Location)
	}
	key := fmt.Sprintf("%s:%09d:%09d", location.File, location.Line, location.Col)
	c.locations[code][key] = location
}

func (c *riskCollector) result() []verification.RiskArea {
	codes := make([]string, 0, len(c.locations))
	for code := range c.locations {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	risks := make([]verification.RiskArea, 0, len(codes))
	for _, code := range codes {
		keys := make([]string, 0, len(c.locations[code]))
		for key := range c.locations[code] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		locations := make([]verification.Location, 0, len(keys))
		for _, key := range keys {
			locations = append(locations, c.locations[code][key])
		}
		definition := riskDefinitions[code]
		risks = append(risks, verification.RiskArea{Code: code, Reason: definition.reason, Guidance: definition.guidance, Locations: locations})
	}
	return risks
}

type uncertaintyCollector struct {
	items map[string]verification.Uncertainty
}

func newUncertaintyCollector() *uncertaintyCollector {
	return &uncertaintyCollector{items: make(map[string]verification.Uncertainty)}
}

func (c *uncertaintyCollector) add(item verification.Uncertainty) {
	location := ""
	if len(item.Locations) > 0 {
		location = fmt.Sprintf("%s:%09d:%09d", item.Locations[0].File, item.Locations[0].Line, item.Locations[0].Col)
	}
	c.items[item.Code+"\x00"+location+"\x00"+item.Message] = item
}

func (c *uncertaintyCollector) hasCode(code string) bool {
	for _, item := range c.items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func (c *uncertaintyCollector) result() []verification.Uncertainty {
	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]verification.Uncertainty, 0, len(keys))
	for _, key := range keys {
		items = append(items, c.items[key])
	}
	return items
}

func assessRisk(analysis verification.ChangeAnalysis) ([]verification.RiskArea, []verification.Uncertainty, error) {
	risks := newRiskCollector()
	uncertainties := newUncertaintyCollector()
	exported := false
	for _, declaration := range analysis.Change.Declarations {
		name := declaration.Name
		if separator := strings.LastIndexByte(name, '.'); separator >= 0 {
			name = name[separator+1:]
		}
		if !ast.IsExported(name) {
			continue
		}
		exported = true
		location := declaration.CurrentLocation
		if location == nil {
			location = declaration.BaseLocation
		}
		if location != nil {
			risks.add("exported_api_change", *location)
		}
	}
	if exported {
		uncertainties.add(verification.Uncertainty{
			Code: "external_consumers", Message: "reverse-import impact is limited to the selected workspace; external consumers of changed exported declarations are not visible",
			Locations: make([]verification.Location, 0),
		})
	}

	for _, changed := range analysis.Change.Files {
		name := filepath.Base(filepath.FromSlash(changed.Path))
		if name == "go.mod" || name == "go.sum" || changed.Path == "go.work" || changed.Path == "go.work.sum" {
			risks.add("module_graph_change", changedFileLocation(changed))
		}
	}
	for _, source := range analysis.Files {
		if filepath.Ext(source.Change.Path) != ".go" {
			continue
		}
		if len(source.CurrentContent) > 0 {
			if err := scanRiskSource(source.Change.Path, source.CurrentContent, source.Change.CurrentRanges, risks); err != nil {
				return nil, nil, fmt.Errorf("scanning risk facts in %q: %w", source.Change.Path, err)
			}
			addSourceUncertainties(source.Change.Path, source.CurrentContent, source.Change.CurrentRanges, uncertainties)
		}
		if len(source.BaseContent) > 0 {
			path := source.Change.PreviousPath
			if path == "" {
				path = source.Change.Path
			}
			if err := scanRiskSource(path, source.BaseContent, source.Change.BaseRanges, risks); err != nil {
				return nil, nil, fmt.Errorf("scanning base risk facts in %q: %w", path, err)
			}
			addSourceUncertainties(path, source.BaseContent, source.Change.BaseRanges, uncertainties)
		}
	}
	for _, target := range analysis.Packages {
		if target.Distance != 0 {
			continue
		}
		if target.Cgo {
			uncertainties.add(verification.Uncertainty{
				Code: "cgo", Message: "an affected package uses cgo; native compiler, platform ABI, and non-Go behavior are outside source impact analysis",
				Locations: make([]verification.Location, 0),
			})
		}
		if target.BuildConstrained && !uncertainties.hasCode("active_build_constraints") {
			uncertainties.add(verification.Uncertainty{
				Code: "active_build_constraints", Message: "an affected package has Go files excluded by the active build configuration; alternate tag, platform, or architecture variants were not executed",
				Locations: make([]verification.Location, 0),
			})
		}
	}
	return risks.result(), uncertainties.result(), nil
}

func scanRiskSource(path string, content []byte, ranges []verification.LineRange, collector *riskCollector) error {
	if len(ranges) == 0 {
		return nil
	}
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, path, content, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	aliases := importAliases(parsed)
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		start := files.Position(node.Pos())
		end := files.Position(node.End())
		if !sourceSpanIntersects(start.Line, end.Line, ranges) {
			return false
		}
		location := verification.Location{File: path, Line: start.Line, Col: start.Column}
		switch item := node.(type) {
		case *ast.GoStmt, *ast.SendStmt, *ast.SelectStmt, *ast.ChanType:
			collector.add("synchronization_change", location)
		case *ast.UnaryExpr:
			if item.Op == token.ARROW {
				collector.add("synchronization_change", location)
			}
		case *ast.IfStmt:
			if isErrorCondition(item.Cond) {
				collector.add("error_flow_change", location)
			}
		case *ast.ReturnStmt:
			if containsErrorExpression(item.Results, aliases) {
				collector.add("error_flow_change", location)
			}
		case *ast.ForStmt, *ast.RangeStmt:
			collector.add("performance_sensitive_change", location)
		case *ast.FuncDecl:
			if strings.HasPrefix(item.Name.Name, "Benchmark") {
				collector.add("performance_sensitive_change", location)
			}
		case *ast.CallExpr:
			classifyRiskCall(item, aliases, location, collector)
		}
		return true
	})
	return nil
}

func classifyRiskCall(call *ast.CallExpr, aliases map[string]string, location verification.Location, collector *riskCollector) {
	if identifier, ok := call.Fun.(*ast.Ident); ok {
		switch identifier.Name {
		case "make", "new", "append":
			collector.add("performance_sensitive_change", location)
		case "close":
			collector.add("synchronization_change", location)
		case "panic", "recover":
			collector.add("error_flow_change", location)
		}
		return
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	root := selectorRoot(selector.X)
	importPath := aliases[root]
	switch {
	case importPath == "sync" || importPath == "sync/atomic" || strings.HasPrefix(importPath, "golang.org/x/sync/"):
		collector.add("synchronization_change", location)
	case importPath == "errors" || (importPath == "fmt" && selector.Sel.Name == "Errorf"):
		collector.add("error_flow_change", location)
	case importPath == "os" && selector.Sel.Name == "Exit":
		collector.add("error_flow_change", location)
	case importPath == "log" && strings.HasPrefix(selector.Sel.Name, "Fatal"):
		collector.add("error_flow_change", location)
		collector.add("observability_boundary_change", location)
	case importPath == "net/http" || strings.HasPrefix(importPath, "golang.org/x/net/http"):
		collector.add("http_boundary_change", location)
	case importPath == "database/sql":
		collector.add("database_boundary_change", location)
	case strings.HasPrefix(importPath, "crypto/") || importPath == "crypto":
		collector.add("crypto_boundary_change", location)
	case strings.HasPrefix(importPath, "encoding/") || strings.Contains(importPath, "protobuf"):
		collector.add("serialization_boundary_change", location)
	case importPath == "log" || importPath == "log/slog" || strings.Contains(importPath, "opentelemetry") || strings.Contains(importPath, "prometheus"):
		collector.add("observability_boundary_change", location)
	case importPath == "reflect" || importPath == "fmt":
		collector.add("performance_sensitive_change", location)
	}
	name := selector.Sel.Name
	switch name {
	case "Lock", "Unlock", "RLock", "RUnlock", "Wait", "Add", "Done", "Signal", "Broadcast":
		collector.add("synchronization_change", location)
	case "ServeHTTP", "Handle", "HandleFunc":
		collector.add("http_boundary_change", location)
	case "Query", "QueryContext", "Exec", "ExecContext", "Begin", "BeginTx", "Commit", "Rollback", "Scan":
		collector.add("database_boundary_change", location)
	}
	if (name == "Marshal" || name == "Unmarshal" || name == "Encode" || name == "Decode") && serializationReceiver(root) {
		collector.add("serialization_boundary_change", location)
	}
	if observabilityReceiver(root) {
		switch name {
		case "Debug", "Info", "Warn", "Error", "Log", "Start", "End", "Record", "Observe", "Inc", "Add":
			collector.add("observability_boundary_change", location)
		}
	}
}

func importAliases(file *ast.File) map[string]string {
	aliases := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path == "C" {
			continue
		}
		name := filepath.Base(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name != "_" && name != "." {
			aliases[name] = path
		}
	}
	return aliases
}

func selectorRoot(expression ast.Expr) string {
	for {
		switch item := expression.(type) {
		case *ast.Ident:
			return item.Name
		case *ast.SelectorExpr:
			expression = item.X
		case *ast.CallExpr:
			expression = item.Fun
		default:
			return ""
		}
	}
}

func isErrorCondition(expression ast.Expr) bool {
	foundErr, foundNil := false, false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		foundErr = foundErr || identifier.Name == "err" || strings.HasSuffix(identifier.Name, "Err")
		foundNil = foundNil || identifier.Name == "nil"
		return true
	})
	return foundErr && foundNil
}

func containsErrorExpression(expressions []ast.Expr, aliases map[string]string) bool {
	found := false
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			if found {
				return false
			}
			if identifier, ok := node.(*ast.Ident); ok && (identifier.Name == "err" || strings.HasSuffix(identifier.Name, "Err")) {
				found = true
				return false
			}
			if call, ok := node.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					path := aliases[selectorRoot(selector.X)]
					found = path == "errors" || (path == "fmt" && selector.Sel.Name == "Errorf")
				}
			}
			return !found
		})
	}
	return found
}

func sourceSpanIntersects(start, end int, ranges []verification.LineRange) bool {
	for _, changed := range ranges {
		if start <= changed.End && end >= changed.Start {
			return true
		}
	}
	return false
}

func changedFileLocation(changed verification.ChangedFile) verification.Location {
	line := 1
	if len(changed.CurrentRanges) > 0 {
		line = changed.CurrentRanges[0].Start
	} else if len(changed.BaseRanges) > 0 {
		line = changed.BaseRanges[0].Start
	}
	return verification.Location{File: changed.Path, Line: line}
}

func addSourceUncertainties(path string, content []byte, ranges []verification.LineRange, collector *uncertaintyCollector) {
	location := verification.Location{File: path, Line: 1}
	if len(ranges) > 0 {
		location.Line = ranges[0].Start
	}
	prefix := string(content)
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	if strings.Contains(prefix, "Code generated") && strings.Contains(prefix, "DO NOT EDIT.") {
		collector.add(verification.Uncertainty{
			Code: "generated_code", Message: "changed generated Go source was analyzed, but its generator and source inputs were not executed or modeled",
			Locations: []verification.Location{location},
		})
	}
	if strings.Contains(prefix, "//go:build ") || strings.Contains(prefix, "// +build ") {
		collector.add(verification.Uncertainty{
			Code: "active_build_constraints", Message: "changed source has build constraints; verification used only the active Go build configuration",
			Locations: []verification.Location{location},
		})
	}
	if strings.Contains(prefix, "//go:generate ") {
		collector.add(verification.Uncertainty{
			Code: "generated_inputs", Message: "changed source declares a generator; generator execution and generated-input reachability were not modeled",
			Locations: []verification.Location{location},
		})
	}
}

func serializationReceiver(value string) bool {
	switch strings.ToLower(value) {
	case "json", "xml", "gob", "yaml", "proto", "encoder", "decoder", "codec":
		return true
	default:
		return false
	}
}

func observabilityReceiver(value string) bool {
	switch strings.ToLower(value) {
	case "log", "logger", "slog", "tracer", "span", "meter", "metric", "metrics":
		return true
	default:
		return false
	}
}
