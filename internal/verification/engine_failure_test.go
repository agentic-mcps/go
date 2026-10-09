package verification_test

import (
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
)

const failureFixtureModule = "module example.test/verify\n\ngo 1.25.0\n"

const failureFixtureCalc = `package calc

func Sign(value int) int {
	return 1
}
`

const failureFixtureChangedCalc = `package calc

func Sign(value int) int {
	if value < 0 {
		return -1
	}
	return 1
}
`

const failureFixtureCalcTest = `package calc

import "testing"

func TestSign(t *testing.T) {
	if Sign(-1) != -1 || Sign(1) != 1 {
		t.Fatal("unexpected sign")
	}
}
`

func TestEngineReportsBuildFailuresWithCompilerText(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		changes      map[string]string
		wantFile     string
		wantMessage  string
		wantBuild    int
		wantTestFail int
	}{
		{
			name:  "test file references undefined identifier",
			files: map[string]string{"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest},
			changes: map[string]string{
				"calc/calc.go": failureFixtureChangedCalc,
				"calc/calc_test.go": `package calc

import "testing"

func TestSign(t *testing.T) {
	if Sign(-1) != undefinedHelper {
		t.Fatal("unexpected sign")
	}
}
`,
			},
			wantBuild: 1, wantFile: "calc/calc_test.go", wantMessage: "undefined: undefinedHelper",
		},
		{
			name: "dependency fails to compile",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"api/api.go":      "package api\n\nimport \"example.test/verify/calc\"\n\nfunc Sign(value int) int { return calc.Sign(value) }\n",
				"api/api_test.go": "package api\n\nimport \"testing\"\n\nfunc TestSign(t *testing.T) { _ = Sign(1) }\n",
			},
			changes:   map[string]string{"calc/calc.go": "package calc\n\nfunc Sign(value int) int {\n\treturn missingSign\n}\n"},
			wantBuild: 1, wantFile: "calc/calc.go", wantMessage: "undefined: missingSign",
		},
		{
			name:  "failing assertion is not a build failure",
			files: map[string]string{"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest},
			changes: map[string]string{
				"calc/calc.go": "package calc\n\nfunc Sign(value int) int {\n\treturn 2\n}\n",
			},
			wantTestFail: 1, wantMessage: "TestSign failed in example.test/verify/calc",
		},
		{
			name: "TestMain fails outside a named test",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/main_test.go": "package calc\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) {\n\tos.Exit(3)\n}\n",
			},
			changes:      map[string]string{"calc/calc.go": failureFixtureChangedCalc},
			wantTestFail: 1, wantMessage: "tests in example.test/verify/calc failed outside a named test",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := verificationRepository(t, test.files)
			base := verificationGit(t, repository, "rev-parse", "HEAD")
			for path, content := range test.changes {
				verificationWrite(t, repository, path, content)
			}

			report := verifyRepository(t, repository, verification.Request{Base: base})
			if report.Result.Status != verification.ResultFindings && report.Result.Status != verification.ResultIncomplete {
				t.Fatalf("result = %#v, want a blocking result", report.Result)
			}
			builds := findingsOfKind(report, "build.failure")
			if len(builds) != test.wantBuild {
				t.Fatalf("build findings = %#v, want %d", report.Findings, test.wantBuild)
			}
			failures := findingsOfKind(report, "test.failure")
			if len(failures) != test.wantTestFail {
				t.Fatalf("test.failure findings = %d (%#v), want %d", len(failures), report.Findings, test.wantTestFail)
			}
			if test.wantBuild == 0 {
				if !strings.Contains(failures[0].Message, test.wantMessage) {
					t.Fatalf("test failure message = %q, want %q", failures[0].Message, test.wantMessage)
				}
				return
			}
			build := builds[0]
			if build.Severity != verification.SeverityError || build.CheckID != "tests" {
				t.Fatalf("build finding = %#v, want blocking tests finding", build)
			}
			if !strings.Contains(build.Message, test.wantMessage) || strings.Contains(build.Message, "exited with status") {
				t.Fatalf("build message = %q, want compiler text %q", build.Message, test.wantMessage)
			}
			if build.Location == nil || build.Location.File != test.wantFile || build.Location.Line == 0 || build.Location.Col == 0 {
				t.Fatalf("build location = %#v, want %s position", build.Location, test.wantFile)
			}
			tests := evidenceByKind(t, report, verification.CheckTests)
			if tests.Status != verification.EvidenceFailed || tests.Tests == nil {
				t.Fatalf("test evidence = %#v, want failed", tests)
			}
			for _, pkg := range tests.Tests.Packages {
				if pkg.Status == "FAIL" && !strings.Contains(pkg.Output, test.wantMessage) {
					t.Fatalf("package summary %#v lacks compiler text", pkg)
				}
			}
			if report.Result.BlockingFindings == 0 {
				t.Fatalf("result = %#v, want the build failure to block", report.Result)
			}
		})
	}
}

func TestEngineCoverageIsUnavailableWhenOwningPackageTestsAbort(t *testing.T) {
	panicking := `package calc

import "testing"

func TestUnrelatedPanic(t *testing.T) { panic("pre-existing bug") }
`
	passingUtil := map[string]string{
		"util/util.go":      "// Package util is a fixture.\npackage util\n\nfunc Double(value int) int { return value * 2 }\n",
		"util/util_test.go": "package util\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"double\")\n\t}\n}\n",
	}
	tests := []struct {
		files           map[string]string
		changes         map[string]string
		name            string
		wantSummary     string
		wantUncertainty string
		wantStatus      verification.EvidenceStatus
	}{
		{
			name: "unrelated panicking test in the changed package",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/panic_test.go": panicking,
			},
			wantStatus: verification.EvidenceError, wantSummary: "coverage unavailable: tests in example.test/verify/calc failed",
		},
		{
			name: "panicking package beside a passing changed package",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/panic_test.go": panicking,
				"util/util.go":       passingUtil["util/util.go"], "util/util_test.go": passingUtil["util/util_test.go"],
			},
			changes:    map[string]string{"util/util.go": strings.Replace(passingUtil["util/util.go"], "is a fixture", "is a changed fixture", 1)},
			wantStatus: verification.EvidenceError, wantSummary: "coverage unavailable: tests in example.test/verify/calc failed",
		},
		{
			name: "reverse importer coverage does not stand in for a panicking package",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/panic_test.go": panicking,
				"api/api.go":         "package api\n\nimport \"example.test/verify/calc\"\n\nfunc Sign(value int) int { return calc.Sign(value) }\n",
				"api/api_test.go":    "package api\n\nimport \"testing\"\n\nfunc TestSign(t *testing.T) {\n\tif Sign(-1) != -1 || Sign(1) != 1 {\n\t\tt.Fatal(\"sign\")\n\t}\n}\n",
			},
			wantStatus: verification.EvidenceError, wantSummary: "coverage unavailable: tests in example.test/verify/calc failed",
		},
		{
			name: "passing changed package still counts beside a panicking one",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/panic_test.go": panicking,
				"util/util.go":       passingUtil["util/util.go"], "util/util_test.go": passingUtil["util/util_test.go"],
			},
			changes:    map[string]string{"util/util.go": strings.Replace(passingUtil["util/util.go"], "value * 2", "value + value", 1)},
			wantStatus: verification.EvidencePassed, wantSummary: "100.0% of changed statements covered",
			wantUncertainty: "coverage_incomplete",
		},
		{
			name: "failing assertion still writes coverage",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"calc/fail_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestUnrelatedFailure(t *testing.T) { t.Fatal(\"pre-existing failure\") }\n",
			},
			wantStatus: verification.EvidencePassed, wantSummary: "100.0% of changed statements covered",
		},
		{
			name: "panic in a package without changed statements",
			files: map[string]string{
				"go.mod": failureFixtureModule, "calc/calc.go": failureFixtureCalc, "calc/calc_test.go": failureFixtureCalcTest,
				"api/api.go":        "package api\n\nimport \"example.test/verify/calc\"\n\nfunc Sign(value int) int { return calc.Sign(value) }\n",
				"api/panic_test.go": strings.Replace(panicking, "package calc", "package api", 1),
			},
			wantStatus: verification.EvidencePassed, wantSummary: "100.0% of changed statements covered",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := verificationRepository(t, test.files)
			base := verificationGit(t, repository, "rev-parse", "HEAD")
			verificationWrite(t, repository, "calc/calc.go", failureFixtureChangedCalc)
			for path, content := range test.changes {
				verificationWrite(t, repository, path, content)
			}

			report := verifyRepository(t, repository, verification.Request{Base: base})
			coverage := evidenceByKind(t, report, verification.CheckCoverage)
			if coverage.Status != test.wantStatus || coverage.Summary != test.wantSummary {
				t.Fatalf("coverage evidence = %#v, want %s %q", coverage, test.wantStatus, test.wantSummary)
			}
			gotUncertainty := ""
			for _, item := range report.Uncertainties {
				if item.Code == "coverage_incomplete" {
					gotUncertainty = item.Code
				}
			}
			if gotUncertainty != test.wantUncertainty {
				t.Fatalf("uncertainties = %#v, want coverage_incomplete=%t", report.Uncertainties, test.wantUncertainty != "")
			}
			if test.wantStatus == verification.EvidenceError {
				if coverage.Coverage != nil {
					t.Fatalf("coverage payload = %#v, want none for unavailable coverage", coverage.Coverage)
				}
				if report.Result.Status != verification.ResultIncomplete {
					t.Fatalf("result = %#v, want incomplete", report.Result)
				}
				return
			}
			if coverage.Coverage == nil || coverage.Coverage.TotalStatements == 0 || coverage.Coverage.CoveredStatements != coverage.Coverage.TotalStatements {
				t.Fatalf("coverage payload = %#v, want fully covered changed statements", coverage.Coverage)
			}
		})
	}
}

func findingsOfKind(report verification.Report, kind string) []verification.Finding {
	result := make([]verification.Finding, 0)
	for _, item := range report.Findings {
		if item.Kind == kind {
			result = append(result, item)
		}
	}
	return result
}
