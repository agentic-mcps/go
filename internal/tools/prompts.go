package tools

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterPrompts registers the six workflow prompts.
func RegisterPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{Name: "audit-package", Description: "Combine concurrency and error-handling audit findings for a Go package.", Arguments: requiredArgs("package", "Go package import path or ./relative/path")}, promptHandler("audit-package", []string{"package"}, auditPackageTemplate))
	server.AddPrompt(&mcp.Prompt{Name: "pre-commit-check", Description: "Run tests, race detection, and coverage checks before committing a Go package.", Arguments: []*mcp.PromptArgument{{Name: "package", Description: "Go package import path or ./relative/path", Required: true}, {Name: "coverage_threshold", Description: "Minimum required overall coverage percentage", Required: true}}}, promptHandler("pre-commit-check", []string{"package", "coverage_threshold"}, preCommitTemplate))
	server.AddPrompt(&mcp.Prompt{Name: "bisect-flake", Description: "Investigate flaky Go tests and correlate them with race reports.", Arguments: []*mcp.PromptArgument{{Name: "package", Description: "Go package import path or ./relative/path", Required: true}, {Name: "runs", Description: "Number of repeated flake-finder runs", Required: true}}}, promptHandler("bisect-flake", []string{"package", "runs"}, bisectFlakeTemplate))
	server.AddPrompt(&mcp.Prompt{Name: "verify-change", Description: "Verify a local Go change once and interpret its source-grounded report.", Arguments: requiredArgs("base", "Local commit or ref to compare with HEAD and the final worktree")}, promptHandler("verify-change", []string{"base"}, verifyChangeTemplate))
	server.AddPrompt(&mcp.Prompt{Name: "understand-change", Description: "Begin a private Change Contract and orient an agent before editing.", Arguments: []*mcp.PromptArgument{{Name: "base", Required: true}, {Name: "goal", Required: true}}}, promptHandler("understand-change", []string{"base", "goal"}, understandChangeTemplate))
	server.AddPrompt(&mcp.Prompt{Name: "resume-change", Description: "Resume from the current private Change Contract and checkpoint before editing.", Arguments: nil}, promptHandler("resume-change", nil, resumeChangeTemplate))
}

func requiredArgs(name, description string) []*mcp.PromptArgument {
	return []*mcp.PromptArgument{{Name: name, Description: description, Required: true}}
}

func promptHandler(name string, args []string, tmpl *template.Template) mcp.PromptHandler {
	return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		if req == nil || req.Params == nil {
			return nil, fmt.Errorf("%s prompt: request parameters are required", name)
		}
		values := make(map[string]string, len(args))
		for _, arg := range args {
			value, ok := req.Params.Arguments[arg]
			if !ok || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%s prompt: argument %q is required and must be nonblank", name, arg)
			}
			values[arg] = value
		}
		if err := validatePromptValues(name, values); err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, values); err != nil {
			return nil, fmt.Errorf("rendering %s prompt: %w", name, err)
		}
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: buf.String()}}}}, nil
	}
}

func validatePromptValues(name string, values map[string]string) error {
	if base := values["base"]; strings.HasPrefix(base, "-") || strings.IndexFunc(base, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%s prompt: argument %q is not a valid local commit or ref", name, "base")
	}
	if pkg := values["package"]; strings.HasPrefix(pkg, "-") || strings.IndexFunc(pkg, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%s prompt: argument %q is not a valid package pattern", name, "package")
	}
	if value, ok := values["coverage_threshold"]; ok {
		threshold, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
			return fmt.Errorf("%s prompt: argument %q must be a number from 0 through 100", name, "coverage_threshold")
		}
	}
	if value, ok := values["runs"]; ok {
		runs, err := strconv.Atoi(value)
		if err != nil || runs < 1 || runs > 200 {
			return fmt.Errorf("%s prompt: argument %q must be an integer from 1 through 200", name, "runs")
		}
	}
	return nil
}

var auditPackageTemplate = template.Must(template.New("audit-package").Parse(`Audit the Go package {{.package}}.
Call go_audit_concurrency and go_audit_errors exactly once for package="{{.package}}". Merge their findings into one result, combine their severity counts, and list every error-severity finding's Location and Message verbatim. Report explicitly when either audit has no findings.`))

var preCommitTemplate = template.Must(template.New("pre-commit-check").Parse(`Run the pre-commit checks for Go package {{.package}}.
1. Call go_test_structured with package="{{.package}}".
2. Call go_race_report with package="{{.package}}".
3. Call go_coverage_gaps with package="{{.package}}".
Fail explicitly if any test failed, any race conflict was found, or OverallPercent < {{.coverage_threshold}}. Do not soften a failure or omit a clean result.`))

var bisectFlakeTemplate = template.Must(template.New("bisect-flake").Parse(`Investigate flaky tests in Go package {{.package}}.
Call go_flake_finder with package="{{.package}}" and runs={{.runs}}. For each name in Flaky, call go_race_report on the same package and cross-reference whether any RaceConflict.Current.Function matches the flaky test's package. State the correlation explicitly when found; otherwise state "no race correlation found". Do not omit the negative result.`))

var verifyChangeTemplate = template.Must(template.New("verify-change").Parse(`Verify the current Go repository change against base {{.base}}.
Call go_verify_change exactly once with base="{{.base}}". Treat the returned policy result as automation state, not a safety verdict. Summarize the changed surface, affected packages, executed evidence, introduced findings, change-grounded risk guidance, and explicit uncertainties. Distinguish failed checks from unavailable evidence, retain source locations, and do not infer absence of risk from an absent trigger.`))

var understandChangeTemplate = template.Must(template.New("understand-change").Parse(`Begin a private change contract for base {{.base}} and goal "{{.goal}}".
Call go_begin_change exactly once with base="{{.base}}" and goal="{{.goal}}". Then use source-grounded context to understand the workspace before editing. The contract does not edit source.`))
var resumeChangeTemplate = template.Must(template.New("resume-change").Parse(`Read agentic-go://change-contract/current before continuing.
Inspect the current private Change Contract and call go_checkpoint_change exactly once with its contract_id and expected_snapshot_id. Treat stale snapshots and policy violations as signals to stop and reorient before editing.`))
