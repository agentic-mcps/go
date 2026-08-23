package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type moduleResource struct {
	Module    string `json:"module"`
	GoVersion string `json:"go_version"`
	Requires  []struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	} `json:"requires"`
}

type packageResource struct {
	ImportPath  string `json:"import_path"`
	Name        string `json:"name"`
	GoFiles     int    `json:"go_files"`
	TestGoFiles int    `json:"test_go_files"`
}

type analysisRuleResource struct {
	Rule      string `json:"rule"`
	Domain    string `json:"domain"`
	Severity  string `json:"severity"`
	SourceDoc string `json:"source_doc"`
}

// RegisterWorkspaceResources registers the v0.1 workspace context resources.
func RegisterWorkspaceResources(server *mcp.Server, runtime *Runtime) {
	server.AddResource(&mcp.Resource{Name: "module", Description: "Current module path, Go version, and direct dependency requirements.", URI: "agentic-go://module", MIMEType: "application/json"}, runtime.moduleResource)
	server.AddResource(&mcp.Resource{Name: "packages", Description: "Current workspace package and source-file inventory.", URI: "agentic-go://packages", MIMEType: "application/json"}, runtime.packagesResource)
	server.AddResource(&mcp.Resource{Name: "analysis-rules", Description: "Registered concurrency and error audit rules with documentation references.", URI: "agentic-go://analysis-rules", MIMEType: "application/json"}, runtime.analysisRulesResource)
}

func resourceURI(req *mcp.ReadResourceRequest, want string) error {
	if req == nil || req.Params == nil || req.Params.URI != want {
		return fmt.Errorf("resource URI must be %q", want)
	}
	return nil
}

func jsonResource(uri string, value any) (*mcp.ReadResourceResult, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding resource: %w", err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(payload)}}}, nil
}

func (r *Runtime) moduleResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(req, "agentic-go://module"); err != nil {
		return nil, err
	}
	var out, errOut bytes.Buffer
	result, err := r.runner.Run(ctx, execution.Command{Name: "go", Args: []string{"mod", "edit", "-json"}, Env: map[string]string{"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local"}}, execution.Streams{Stdout: &out, Stderr: &errOut})
	if err != nil {
		return nil, fmt.Errorf("reading module metadata: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("go mod edit exited %d: %s", result.ExitCode, boundedMessage(errOut.String()))
	}
	var raw struct {
		Module  struct{ Path string }            `json:"Module"`
		Go      string                           `json:"Go"`
		Require []struct{ Path, Version string } `json:"Require"`
	}
	if err := json.NewDecoder(&out).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding module metadata: %w", err)
	}
	res := moduleResource{Module: raw.Module.Path, GoVersion: raw.Go, Requires: make([]struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	}, 0, len(raw.Require))}
	for _, dep := range raw.Require {
		res.Requires = append(res.Requires, struct {
			Path    string `json:"path"`
			Version string `json:"version"`
		}{dep.Path, dep.Version})
	}
	sort.Slice(res.Requires, func(i, j int) bool { return res.Requires[i].Path < res.Requires[j].Path })
	return jsonResource(req.Params.URI, res)
}

func (r *Runtime) packagesResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(req, "agentic-go://packages"); err != nil {
		return nil, err
	}
	var out, errOut bytes.Buffer
	result, err := r.runner.Run(ctx, execution.Command{Name: "go", Args: []string{"list", "-json", "-mod=readonly", "./..."}, Env: map[string]string{"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local"}}, execution.Streams{Stdout: &out, Stderr: &errOut})
	if err != nil {
		return nil, fmt.Errorf("listing packages: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("go list exited %d: %s", result.ExitCode, boundedMessage(errOut.String()))
	}
	dec := json.NewDecoder(&out)
	packages := make([]packageResource, 0)
	for {
		var raw struct {
			ImportPath, Name                             string
			GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
		}
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decoding package metadata: %w", err)
		}
		packages = append(packages, packageResource{raw.ImportPath, raw.Name, len(raw.GoFiles) + len(raw.CgoFiles), len(raw.TestGoFiles) + len(raw.XTestGoFiles)})
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	return jsonResource(req.Params.URI, packages)
}

func (r *Runtime) analysisRulesResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(req, "agentic-go://analysis-rules"); err != nil {
		return nil, err
	}
	rules := make([]analysisRuleResource, 0)
	for _, domain := range []string{"concurrency", "errors"} {
		for _, rule := range astutil.RulesInDomain(domain) {
			rules = append(rules, analysisRuleResource{rule, domain, string(astutil.RuleSeverity(rule)), fmt.Sprintf("docs/phase-4a-%s.md#%s", domain, rule)})
		}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Rule < rules[j].Rule })
	return jsonResource(req.Params.URI, rules)
}
