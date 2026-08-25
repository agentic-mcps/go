package verification

import (
	"sort"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/parser"
)

type verificationTestCollector struct {
	packages      map[string]TestPackageSummary
	testOutput    map[string]*strings.Builder
	packageOutput map[string]*strings.Builder
	allOutput     map[string]*strings.Builder
	terminal      map[string]struct{}
	nonpassing    []TestCaseSummary
	passed        int
	failed        int
	skipped       int
}

func newVerificationTestCollector() *verificationTestCollector {
	return &verificationTestCollector{
		packages: make(map[string]TestPackageSummary), testOutput: make(map[string]*strings.Builder),
		packageOutput: make(map[string]*strings.Builder), allOutput: make(map[string]*strings.Builder), terminal: make(map[string]struct{}),
		nonpassing: make([]TestCaseSummary, 0),
	}
}

func (c *verificationTestCollector) consume(event parser.TestEvent) error {
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
			if builder := c.packageOutput[event.Package]; builder != nil {
				summary.Output = builder.String()
			}
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

func (c *verificationTestCollector) result() (TestSummary, map[string]string) {
	packages := make([]TestPackageSummary, 0, len(c.packages))
	texts := make(map[string]string, len(c.allOutput))
	for pkg, summary := range c.packages {
		packages = append(packages, summary)
		if builder := c.allOutput[pkg]; builder != nil {
			texts[pkg] = builder.String()
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
	}, texts
}
