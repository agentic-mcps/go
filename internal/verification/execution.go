package verification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/parser"
)

const (
	verificationTestTimeout = 60 * time.Second
	maxCoverageProfileSize  = 8 << 20
)

type executionOutcome struct {
	Evidence      []Evidence
	Findings      []Finding
	Uncertainties []Uncertainty
}

type affectedRun struct {
	coverageErr error
	facts       testRunFacts
	profile     []parser.CoverageBlock
	race        parser.RaceReportOutput
	tests       TestSummary
	result      execution.Result
	// coverageEmpty reports that go test wrote a profile with no blocks, which
	// happens when every test binary exited before writing coverage.
	coverageEmpty bool
}

func (e *Engine) runAffectedChecks(ctx context.Context, analysis ChangeAnalysis, request Request, direct []string) (executionOutcome, error) {
	if len(analysis.Packages) == 0 {
		evidence := make([]Evidence, 0, 3)
		for _, check := range executionChecks([]string{}, request.Race) {
			evidence = append(evidence, Evidence{
				CheckID: check.ID, Kind: check.Kind, Status: EvidenceSkipped,
				Summary: "no active affected packages to execute",
			})
		}
		return executionOutcome{Evidence: evidence, Findings: []Finding{}, Uncertainties: []Uncertainty{}}, nil
	}
	coverageApplicable := e.hasChangedExecutableStatements(analysis)
	run, err := e.executeGoTest(ctx, analysis.Packages, direct, request, coverageApplicable)
	if err != nil {
		return executionOutcome{}, err
	}
	outcome := executionOutcome{
		Evidence: make([]Evidence, 0, 3), Findings: make([]Finding, 0), Uncertainties: make([]Uncertainty, 0),
	}
	testStatus := EvidencePassed
	if run.tests.Failed > 0 || run.result.ExitCode != 0 {
		testStatus = EvidenceFailed
	}
	outcome.Evidence = append(outcome.Evidence, Evidence{
		CheckID: "tests", Kind: CheckTests, Status: testStatus, DurationMS: run.result.Duration.Milliseconds(),
		Summary: fmt.Sprintf("%d passed, %d failed, %d skipped", run.tests.Passed, run.tests.Failed, run.tests.Skipped),
		Tests:   &run.tests,
	})
	outcome.Findings = append(outcome.Findings, e.testFailureFindings(run.tests, run.facts, run.result.ExitCode)...)

	if !coverageApplicable {
		outcome.Evidence = append(outcome.Evidence, Evidence{
			CheckID: "coverage", Kind: CheckCoverage, Status: EvidenceSkipped,
			DurationMS: run.result.Duration.Milliseconds(),
			Summary:    "no added or modified executable Go statements",
		})
	} else {
		coverageEvidence, coverageUncertainty := e.coverageEvidence(analysis, run)
		outcome.Evidence = append(outcome.Evidence, coverageEvidence)
		outcome.Uncertainties = append(outcome.Uncertainties, coverageUncertainty...)
	}

	if request.Race {
		raceStatus := EvidencePassed
		if len(run.race.Conflicts) > 0 {
			raceStatus = EvidenceFailed
		}
		outcome.Evidence = append(outcome.Evidence, Evidence{
			CheckID: "race", Kind: CheckRace, Status: raceStatus, DurationMS: run.result.Duration.Milliseconds(),
			Summary: fmt.Sprintf("%d race conflicts observed", len(run.race.Conflicts)),
			Race:    &RaceSummary{Conflicts: len(run.race.Conflicts)},
		})
		outcome.Findings = append(outcome.Findings, e.raceFindings(run.race)...)
	}
	return outcome, nil
}

func (e *Engine) executeGoTest(ctx context.Context, targets []ExecutionTarget, direct []string, request Request, coverage bool) (affectedRun, error) {
	runDir, err := createVerificationRunDir("verify")
	if err != nil {
		return affectedRun{}, err
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	profilePath := filepath.Join(runDir, "coverage.out")
	args := goTestArgs(request, targets, direct, profilePath, coverage)

	collector := newVerificationTestCollector()
	reader, writer := io.Pipe()
	parsed := make(chan error, 1)
	go func() {
		defer func() { _ = reader.Close() }()
		_, parseErr := parser.DecodeTestJSON(reader, collector.consume)
		parsed <- parseErr
	}()
	var stderr bytes.Buffer
	result, runErr := e.runner.Run(ctx, execution.Command{
		Name: "go", Args: args,
		Env: map[string]string{"GOWORK": "auto", "GOTOOLCHAIN": "local"},
	}, execution.Streams{Stdout: writer, Stderr: &stderr})
	_ = writer.CloseWithError(runErr)
	parseErr := <-parsed
	if runErr != nil {
		return affectedRun{}, fmt.Errorf("running affected package tests: %w", runErr)
	}
	if parseErr != nil {
		return affectedRun{}, fmt.Errorf("parsing affected package tests (exit %d): %w", result.ExitCode, parseErr)
	}

	var profile []parser.CoverageBlock
	var profileErr error
	if coverage {
		profile, profileErr = readCoverageProfile(profilePath)
	}
	tests, packageText, facts := collector.result()
	portableRoots := []string{runDir, e.workspace.Root()}
	if cache, cacheErr := os.UserCacheDir(); cacheErr == nil {
		portableRoots = append(portableRoots, cache)
	}
	sanitizeTestSummary(&tests, portableRoots...)
	for index := range facts.builds {
		facts.builds[index].output = portableReportText(facts.builds[index].output, portableRoots...)
	}
	coverageEmpty := errors.Is(profileErr, parser.ErrNoCoverageBlocks)
	if profileErr != nil {
		profileErr = errors.New(portableCheckError(profileErr, portableRoots...))
	}
	raceText := make([]string, 0, len(packageText))
	packages := make([]string, 0, len(packageText))
	for pkg := range packageText {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	for _, pkg := range packages {
		raceText = append(raceText, packageText[pkg])
	}
	return affectedRun{
		result: result, tests: tests, facts: facts, profile: profile, coverageErr: profileErr, coverageEmpty: coverageEmpty,
		race: parser.Parse(strings.Join(raceText, "\n")),
	}, nil
}

// goTestArgs builds the go test argv for one affected-package run.
func goTestArgs(request Request, targets []ExecutionTarget, direct []string, profilePath string, coverage bool) []string {
	args := []string{"test", "-json"}
	if !request.TestCache {
		args = append(args, "-count=1")
	}
	args = append(args, fmt.Sprintf("-timeout=%ds", int(verificationTestTimeout.Seconds())))
	if request.Short {
		args = append(args, "-short")
	}
	if request.Skip != "" {
		args = append(args, "-skip="+request.Skip)
	}
	if coverage {
		args = append(args, "-covermode=atomic", "-coverprofile="+profilePath)
		if len(direct) > 0 {
			args = append(args, "-coverpkg="+strings.Join(direct, ","))
		}
	}
	if request.Race {
		args = append(args, "-race")
	}
	for _, target := range targets {
		args = append(args, target.ID)
	}
	return args
}

func readCoverageProfile(path string) ([]parser.CoverageBlock, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspecting changed coverage profile: %w", err)
	}
	if info.Size() > maxCoverageProfileSize {
		return nil, fmt.Errorf("changed coverage profile exceeds %d bytes", maxCoverageProfileSize)
	}
	profile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening changed coverage profile: %w", err)
	}
	blocks, parseErr := parser.ParseCoverageBlocks(profile)
	closeErr := profile.Close()
	if parseErr != nil {
		return nil, fmt.Errorf("parsing changed coverage profile: %w", parseErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing changed coverage profile: %w", closeErr)
	}
	return blocks, nil
}

func createVerificationRunDir(prefix string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating user cache: %w", err)
	}
	runs := filepath.Join(cache, "agentic-go", "runs")
	if mkdirErr := os.MkdirAll(runs, 0o700); mkdirErr != nil {
		return "", fmt.Errorf("creating run cache: %w", mkdirErr)
	}
	directory, err := os.MkdirTemp(runs, prefix+"-")
	if err != nil {
		return "", fmt.Errorf("creating verification run: %w", err)
	}
	return directory, nil
}

// testFailureFindings reports each failed named test, each failed build once,
// and each package that failed outside any named test (TestMain, init panic,
// timeout), falling back to the process status only when nothing is attributed.
func (e *Engine) testFailureFindings(summary TestSummary, facts testRunFacts, exitCode int) []Finding {
	findings := make([]Finding, 0, summary.Failed+len(facts.builds))
	for _, test := range summary.Nonpassing {
		if test.Status != "fail" {
			continue
		}
		message := fmt.Sprintf("%s failed in %s", test.Name, test.Package)
		if output := strings.TrimSpace(test.Output); output != "" {
			message += ": " + boundedText(output, 512)
		}
		findings = append(findings, Finding{Kind: "test.failure", Severity: SeverityError, Message: message, CheckID: "tests"})
	}
	for _, build := range facts.builds {
		findings = append(findings, e.buildFailureFinding(build))
	}
	for _, pkg := range summary.Packages {
		if _, built := facts.buildFailed[pkg.Package]; built || pkg.Status != "FAIL" || (pkg.Failed > 0 && !abortedOutsideTests(pkg.Output)) {
			continue
		}
		message := fmt.Sprintf("tests in %s failed outside a named test", pkg.Package)
		if output := strings.TrimSpace(pkg.Output); output != "" {
			message += ": " + boundedText(output, 512)
		}
		findings = append(findings, Finding{Kind: "test.failure", Severity: SeverityError, Message: message, CheckID: "tests"})
	}
	if len(findings) == 0 && exitCode != 0 {
		findings = append(findings, Finding{
			Kind: "test.failure", Severity: SeverityError, CheckID: "tests",
			Message: fmt.Sprintf("affected package test process exited with status %d", exitCode),
		})
	}
	return findings
}

// abortedOutsideTests reports whether package-level output (output not
// attributed to a named test) shows the test binary crashing, as when TestMain
// panics after m.Run. A panic inside a named test is attributed to that test.
func abortedOutsideTests(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ") {
			return true
		}
	}
	return false
}

func sanitizeTestSummary(summary *TestSummary, roots ...string) {
	if summary == nil {
		return
	}
	for index := range summary.Packages {
		summary.Packages[index].Output = boundedText(portableReportText(summary.Packages[index].Output, roots...), 4096)
	}
	for index := range summary.Nonpassing {
		summary.Nonpassing[index].Output = boundedText(portableReportText(summary.Nonpassing[index].Output, roots...), 4096)
	}
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
