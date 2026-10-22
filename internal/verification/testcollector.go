package verification

import (
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/parser"
)

type verificationTestCollector struct {
	packages         map[string]TestPackageSummary
	testOutput       map[string]*strings.Builder
	packageOutput    map[string]*strings.Builder
	allOutput        map[string]*strings.Builder
	buildOutput      map[string]*strings.Builder
	failedBuild      map[string]string
	coverageReported map[string]struct{}
	terminal         map[string]struct{}
	nonpassing       []TestCaseSummary
	passed           int
	failed           int
	skipped          int
}

// buildFailure is one failed compilation reported by go test -json. Every test
// package that could not run because of it names it in FailedBuild.
type buildFailure struct {
	importPath string
	output     string
}

// testRunFacts retains package-level facts that the portable TestSummary
// cannot represent without a schema change.
type testRunFacts struct {
	// coverageLost holds failed packages that never reported coverage, so
	// their test binary wrote no profile (panic, build failure, timeout).
	coverageLost map[string]struct{}
	// buildFailed holds test packages that could not run because a build failed.
	buildFailed map[string]struct{}
	builds      []buildFailure
}

func newVerificationTestCollector() *verificationTestCollector {
	return &verificationTestCollector{
		packages: make(map[string]TestPackageSummary), testOutput: make(map[string]*strings.Builder),
		packageOutput: make(map[string]*strings.Builder), allOutput: make(map[string]*strings.Builder),
		buildOutput: make(map[string]*strings.Builder), failedBuild: make(map[string]string),
		coverageReported: make(map[string]struct{}), terminal: make(map[string]struct{}),
		nonpassing: make([]TestCaseSummary, 0),
	}
}

func (c *verificationTestCollector) consume(event parser.TestEvent) error {
	if event.Action == "build-output" || event.Action == "build-fail" {
		builder := c.buildOutput[event.ImportPath]
		if builder == nil {
			builder = &strings.Builder{}
			c.buildOutput[event.ImportPath] = builder
		}
		builder.WriteString(event.Output)
		return nil
	}
	if event.Package == "" {
		return nil
	}
	if _, exists := c.packages[event.Package]; !exists {
		c.packages[event.Package] = TestPackageSummary{Package: event.Package}
	}
	if event.Action == "output" {
		all := c.allOutput[event.Package]
		if all == nil {
			all = &strings.Builder{}
			c.allOutput[event.Package] = all
		}
		all.WriteString(event.Output)
		if event.Test == "" {
			if strings.HasPrefix(event.Output, "coverage:") {
				c.coverageReported[event.Package] = struct{}{}
			}
			builder := c.packageOutput[event.Package]
			if builder == nil {
				builder = &strings.Builder{}
				c.packageOutput[event.Package] = builder
			}
			builder.WriteString(event.Output)
			return nil
		}
		key := event.Package + "\x00" + event.Test
		builder := c.testOutput[key]
		if builder == nil {
			builder = &strings.Builder{}
			c.testOutput[key] = builder
		}
		builder.WriteString(event.Output)
		return nil
	}
	if event.Action != "pass" && event.Action != "fail" && event.Action != "skip" {
		return nil
	}
	if event.Test == "" {
		summary := c.packages[event.Package]
		switch event.Action {
		case "pass":
			summary.Status = "ok"
		case "skip":
			summary.Status = "skip"
		case "fail":
			summary.Status = "FAIL"
			summary.Output = c.failedPackageOutput(event)
		}
		c.packages[event.Package] = summary
		return nil
	}
	key := event.Package + "\x00" + event.Test
	if _, exists := c.terminal[key]; exists {
		return nil
	}
	c.terminal[key] = struct{}{}
	summary := c.packages[event.Package]
	switch event.Action {
	case "pass":
		c.passed++
		summary.Passed++
	case "fail":
		c.failed++
		summary.Failed++
	case "skip":
		c.skipped++
		summary.Skipped++
	}
	if event.Action != "pass" {
		item := TestCaseSummary{Package: event.Package, Name: event.Test, Status: event.Action, ElapsedS: event.Elapsed}
		if event.Action == "fail" {
			if builder := c.testOutput[key]; builder != nil {
				item.Output = builder.String()
			}
		}
		c.nonpassing = append(c.nonpassing, item)
	}
	c.packages[event.Package] = summary
	delete(c.testOutput, key)
	return nil
}

// failedPackageOutput prefixes the compiler output of a failed build so a
// package that could not compile carries the reason, not only "[build failed]".
func (c *verificationTestCollector) failedPackageOutput(event parser.TestEvent) string {
	var output strings.Builder
	if event.FailedBuild != "" {
		c.failedBuild[event.Package] = event.FailedBuild
		if builder := c.buildOutput[event.FailedBuild]; builder != nil {
			output.WriteString(builder.String())
		}
	}
	if builder := c.packageOutput[event.Package]; builder != nil {
		output.WriteString(builder.String())
	}
	return output.String()
}

func (c *verificationTestCollector) result() (TestSummary, map[string]string, testRunFacts) {
	packages := make([]TestPackageSummary, 0, len(c.packages))
	texts := make(map[string]string, len(c.allOutput))
	facts := testRunFacts{
		coverageLost: make(map[string]struct{}), buildFailed: make(map[string]struct{}, len(c.failedBuild)),
		builds: c.buildFailures(),
	}
	for pkg := range c.failedBuild {
		facts.buildFailed[pkg] = struct{}{}
	}
	for pkg, summary := range c.packages {
		packages = append(packages, summary)
		if builder := c.allOutput[pkg]; builder != nil {
			texts[pkg] = builder.String()
		}
		if _, reported := c.coverageReported[pkg]; summary.Status == "FAIL" && !reported {
			facts.coverageLost[pkg] = struct{}{}
		}
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Package < packages[j].Package })
	sort.Slice(c.nonpassing, func(i, j int) bool {
		if c.nonpassing[i].Package != c.nonpassing[j].Package {
			return c.nonpassing[i].Package < c.nonpassing[j].Package
		}
		return c.nonpassing[i].Name < c.nonpassing[j].Name
	})
	return TestSummary{
		Passed: c.passed, Failed: c.failed, Skipped: c.skipped,
		Packages: packages, Nonpassing: c.nonpassing,
	}, texts, facts
}

// buildFailures groups failed test packages by the build that broke them, so
// one compile error shared by many dependents is reported once.
func (c *verificationTestCollector) buildFailures() []buildFailure {
	byBuild := make(map[string]buildFailure)
	for _, failed := range c.failedBuild {
		if _, exists := byBuild[failed]; exists {
			continue
		}
		item := buildFailure{importPath: buildImportPath(failed)}
		if builder := c.buildOutput[failed]; builder != nil {
			item.output = builder.String()
		}
		byBuild[failed] = item
	}
	result := make([]buildFailure, 0, len(byBuild))
	for _, item := range byBuild {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].importPath != result[j].importPath {
			return result[i].importPath < result[j].importPath
		}
		return result[i].output < result[j].output
	})
	return result
}

// buildImportPath drops the test-variant suffix go test appends to a build
// import path, as in "example.com/a [example.com/a.test]".
func buildImportPath(value string) string {
	if index := strings.Index(value, " ["); index > 0 {
		return value[:index]
	}
	return value
}
