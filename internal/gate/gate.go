package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	// gateAnalyzeLimit is the largest closure change discovery accepts.
	gateAnalyzeLimit = 500
	// gateRaceRisk is the change-impact risk that turns on race detection in CI.
	gateRaceRisk = "synchronization_change"
)

// profileSettings are the defaults a profile selects.
type profileSettings struct {
	budget      time.Duration
	maxPackages int
	testCache   bool
	short       bool
	directOnly  bool
	raceOnRisk  bool
}

var profiles = map[Profile]profileSettings{
	ProfileHook:  {budget: 120 * time.Second, maxPackages: 60, testCache: true, short: true, directOnly: true},
	ProfileLocal: {budget: 10 * time.Minute, maxPackages: 200, testCache: true, directOnly: true},
	ProfileCI:    {budget: 20 * time.Minute, maxPackages: 500, raceOnRisk: true},
}

// Gate runs the done-gate for one workspace.
type Gate struct {
	ws       *workspace.Workspace
	runner   *execution.Runner
	store    *Store
	analyzer *changeimpact.Analyzer
	engine   *verification.Engine
	git      GitFunc
	getenv   func(string) string
	version  string
}

// New builds a gate for ws. store may be nil, which disables the result cache
// and the run lock.
func New(ws *workspace.Workspace, runner *execution.Runner, version string, store *Store) (*Gate, error) {
	if ws == nil {
		return nil, errors.New("workspace is nil")
	}
	if runner == nil {
		return nil, errors.New("runner is nil")
	}
	analyzer, err := changeimpact.New(ws, runner)
	if err != nil {
		return nil, fmt.Errorf("creating change analyzer: %w", err)
	}
	engine, err := verification.NewEngine(ws, runner, analyzer, version)
	if err != nil {
		return nil, fmt.Errorf("creating verification engine: %w", err)
	}
	return &Gate{
		ws: ws, runner: runner, store: store, analyzer: analyzer, engine: engine,
		git: NewGit(runner, ws.Root()), getenv: os.Getenv, version: version,
	}, nil
}

// gateRun is the state of one Run.
type gateRun struct {
	result      Result
	fingerprint string
	start       time.Time
	options     Options
	settings    profileSettings
	cacheable   bool
	complete    bool
}

// Run checks the change against its base and returns the verdict. It returns
// an error only for caller cancellation or setup failures (no repository, an
// unresolvable base, an invalid profile); every evidence problem becomes a
// note and makes the verdict unknown unless something blocks.
func (g *Gate) Run(ctx context.Context, options Options) (Result, error) {
	run, err := g.newRun(options)
	if err != nil {
		return Result{}, err
	}
	result, err := g.run(ctx, run)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, fmt.Errorf("gate run canceled: %w", ctxErr)
		}
		return Result{}, err
	}
	return result, nil
}

func (g *Gate) newRun(options Options) (*gateRun, error) {
	if options.Profile == "" {
		options.Profile = ProfileLocal
	}
	settings, ok := profiles[options.Profile]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", options.Profile)
	}
	if options.Budget <= 0 {
		options.Budget = settings.budget
	}
	if options.MaxPackages <= 0 {
		options.MaxPackages = settings.maxPackages
	}
	options.MaxPackages = min(options.MaxPackages, gateAnalyzeLimit)
	return &gateRun{
		start: time.Now(), options: options, settings: settings, complete: true,
		result: Result{SchemaVersion: SchemaVersion, Items: []Item{}, Notes: []string{}},
	}, nil
}

func (g *Gate) run(ctx context.Context, run *gateRun) (Result, error) {
	base, err := DetectBase(ctx, g.git, run.options.Base, run.options.SessionBase, g.getenv)
	if err != nil {
		return Result{}, fmt.Errorf("detecting base: %w", err)
	}
	run.result.Base = base
	changed, err := ChangedFiles(ctx, g.git, base.Commit, true)
	if err != nil {
		return Result{}, err
	}
	run.result.Stats.ChangedFiles = len(changed)
	changedGo := gateGoFiles(changed)
	run.result.Stats.ChangedGoFiles = len(changedGo)
	if len(changed) == 0 {
		run.note("no changes against " + base.Ref)
		return g.finish(run), nil
	}
	if fingerprint, fpErr := Fingerprint(ctx, g.git, g.ws.Root(), base.Commit, g.salt(run.options)); fpErr == nil {
		run.fingerprint = fingerprint
	}
	if !gateTouchesGo(changed) {
		run.add(CheckIntegrity(gatePathFiles(changed), nil)...)
		return g.finish(run), nil
	}
	run.cacheable = g.store != nil && !run.options.NoCache && run.fingerprint != "" && !gateCacheUnsafe(g.ws.Root())
	if cached, ok := g.cached(run); ok {
		return cached, nil
	}
	if g.store != nil {
		unlock, lockErr := g.lock(ctx, run)
		if lockErr != nil {
			return Result{}, lockErr
		}
		if unlock == nil {
			return g.finish(run), nil
		}
		defer unlock()
		if cached, ok := g.cached(run); ok {
			return cached, nil
		}
	}
	if syntax := CheckSyntax(g.ws.Root(), changedGo); len(syntax) > 0 {
		run.add(syntax...)
		run.note("other checks skipped until the code parses")
		return g.finish(run), nil
	}
	return g.verify(ctx, run, changed)
}

// lock serializes gate runs on the store. A nil unlock without error means
// the time budget ran out while waiting, which is already recorded.
func (g *Gate) lock(ctx context.Context, run *gateRun) (func(), error) {
	lockCtx, cancel := context.WithDeadline(ctx, run.start.Add(run.options.Budget))
	defer cancel()
	unlock, err := g.store.Lock(lockCtx)
	if err == nil {
		return unlock, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	run.note("another agentic-go check run held the lock until the time budget ran out")
	run.complete = false
	run.cacheable = false
	return nil, nil
}

// verify analyzes the change, checks integrity, runs the affected tests and
// attributes failures.
func (g *Gate) verify(ctx context.Context, run *gateRun, changed []string) (Result, error) {
	analysis, err := g.analyzer.Analyze(ctx, verification.ChangeOptions{Base: run.result.Base.Commit, Package: "./...", MaxPackages: gateAnalyzeLimit})
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		run.note("could not analyze the change: " + attributeOneLine(err.Error()))
		run.complete = false
		run.add(CheckIntegrity(gatePathFiles(changed), nil)...)
		return g.finish(run), nil
	}
	run.result.Stats.PackagesAffected = len(analysis.Packages)
	if !analysis.Complete {
		run.note(fmt.Sprintf("affected package closure exceeds %d packages; distant consumers were not tested", gateAnalyzeLimit))
		run.complete = false
		analysis.Complete = true
	}
	analysis.Packages = gateTrim(analysis.Packages, run.options.MaxPackages)
	run.result.Stats.PackagesTested = len(analysis.Packages)
	if run.result.Stats.PackagesTested < run.result.Stats.PackagesAffected {
		run.note(fmt.Sprintf("tested %d of %d affected packages (direct first, then closest consumers)", run.result.Stats.PackagesTested, run.result.Stats.PackagesAffected))
	}
	run.add(CheckIntegrity(analysis.Files, gateDeletedDeclarations(analysis.Change.Declarations))...)

	budgetCtx, cancel := context.WithDeadline(ctx, run.start.Add(run.options.Budget))
	defer cancel()
	request := g.request(run, analysis)
	collection, err := g.engine.CollectAnalysis(budgetCtx, request, analysis)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		run.note(g.collectNote(budgetCtx, run, err))
		run.complete = false
		return g.finish(run), nil
	}
	collected := mapCollection(collection.Report, analysis.Files, run.options.RequireCoverage)
	run.add(collected.items...)
	run.result.Notes = append(run.result.Notes, collected.notes...)
	run.complete = run.complete && collected.complete
	if len(collected.failures) > 0 {
		g.attribute(budgetCtx, run, request, collection.Analysis, collected.failures)
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
	}
	return g.finish(run), nil
}

func (g *Gate) collectNote(budgetCtx context.Context, run *gateRun, err error) string {
	if budgetCtx.Err() != nil {
		return fmt.Sprintf("time budget %s ran out before tests finished", run.options.Budget)
	}
	return "tests did not finish: " + attributeOneLine(err.Error())
}

func (g *Gate) attribute(ctx context.Context, run *gateRun, request verification.Request, analysis verification.ChangeAnalysis, failures []testFailure) {
	targets := make(map[string]verification.ExecutionTarget, len(analysis.Packages))
	for _, target := range analysis.Packages {
		targets[target.ID] = target
	}
	attr := &attributor{
		materialize: g.analyzer.MaterializeBase, runner: g.runner, targets: targets,
		root: g.ws.Root(), baseShort: formatShortCommit(run.result.Base.Commit),
		repository: analysis.Repository, flags: testFlags(request),
	}
	defer attr.close()
	outcome := attr.attribute(ctx, failures)
	run.add(outcome.items...)
	run.result.Notes = append(run.result.Notes, outcome.notes...)
	run.complete = run.complete && outcome.complete
}

// request builds the engine request from the profile and options.
func (g *Gate) request(run *gateRun, analysis verification.ChangeAnalysis) verification.Request {
	race := run.options.Race
	if run.settings.raceOnRisk {
		for _, risk := range analysis.Risks {
			race = race || risk.Code == gateRaceRisk
		}
	}
	return verification.Request{
		Base: run.result.Base.Commit, Package: "./...", MaxPackages: gateAnalyzeLimit,
		Race: race, TestCache: run.settings.testCache, Short: run.settings.short, Skip: run.options.Skip,
		DirectAnalyzersOnly: run.settings.directOnly, SoftAnalyzerFailures: true,
	}
}

// cached returns the stored result for this run's fingerprint, if any.
func (g *Gate) cached(run *gateRun) (Result, bool) {
	if !run.cacheable {
		return Result{}, false
	}
	result, ok := g.store.LoadResult(run.fingerprint)
	if !ok {
		return Result{}, false
	}
	if result.Items == nil {
		result.Items = []Item{}
	}
	if result.Notes == nil {
		result.Notes = []string{}
	}
	result.Stats.Cached = true
	result.Stats.DurationMS = time.Since(run.start).Milliseconds()
	result.Fingerprint = run.fingerprint
	return result, true
}

// finish computes the verdict and stats and stores a definite result.
func (g *Gate) finish(run *gateRun) Result {
	result := run.result
	SortItems(result.Items)
	result.Verdict = ComputeVerdict(result.Items, run.complete)
	result.Fingerprint = run.fingerprint
	result.Stats.DurationMS = time.Since(run.start).Milliseconds()
	if run.cacheable && result.Verdict != VerdictUnknown {
		// A cache write failure only costs a future cache miss.
		_ = g.store.SaveResult(run.fingerprint, result)
	}
	return result
}

// salt is the stable string of everything besides the tree that changes a
// result: gate version, toolchain, profile and every option.
func (g *Gate) salt(options Options) string {
	return fmt.Sprintf("v=%q go=%q profile=%q base=%q session=%q budget=%d race=%t cover=%t skip=%q max=%d",
		g.version, g.ws.Toolchain().Version, options.Profile, options.Base, options.SessionBase,
		options.Budget, options.Race, options.RequireCoverage, options.Skip, options.MaxPackages)
}

func (run *gateRun) add(items ...Item) {
	run.result.Items = append(run.result.Items, items...)
}

func (run *gateRun) note(note string) {
	run.result.Notes = append(run.result.Notes, note)
}

// gateTrim keeps every directly changed package, then the closest consumers
// ordered by distance and import path, up to limit packages.
func gateTrim(packages []verification.ExecutionTarget, limit int) []verification.ExecutionTarget {
	ordered := append([]verification.ExecutionTarget(nil), packages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Distance != ordered[j].Distance {
			return ordered[i].Distance < ordered[j].Distance
		}
		return ordered[i].ID < ordered[j].ID
	})
	kept := make([]verification.ExecutionTarget, 0, min(len(ordered), limit))
	for _, target := range ordered {
		if target.Distance == 0 || len(kept) < limit {
			kept = append(kept, target)
		}
	}
	return kept
}

func gateDeletedDeclarations(declarations []verification.ChangedDeclaration) []verification.ChangedDeclaration {
	deleted := make([]verification.ChangedDeclaration, 0)
	for _, declaration := range declarations {
		if declaration.Change == verification.ChangeDeleted {
			deleted = append(deleted, declaration)
		}
	}
	return deleted
}

func gateGoFiles(paths []string) []string {
	files := make([]string, 0, len(paths))
	for _, name := range paths {
		if strings.HasSuffix(name, ".go") {
			files = append(files, name)
		}
	}
	return files
}

// gateTouchesGo reports whether a change can affect Go build or test results.
func gateTouchesGo(paths []string) bool {
	for _, name := range paths {
		switch path.Base(name) {
		case "go.mod", "go.sum", "go.work", "go.work.sum":
			return true
		}
		if strings.HasSuffix(name, ".go") {
			return true
		}
	}
	return false
}

// gatePathFiles describes changed paths without content, enough for the
// path-only integrity rules (golden files and gate configuration).
func gatePathFiles(paths []string) []verification.SourceFile {
	files := make([]verification.SourceFile, 0, len(paths))
	for _, name := range paths {
		files = append(files, verification.SourceFile{Change: verification.ChangedFile{Path: name, Change: verification.ChangeModified}})
	}
	return files
}

// gateCacheUnsafe reports workspaces whose results depend on files outside
// the fingerprint: a go.work, or a go.mod replace pointing at a local path.
func gateCacheUnsafe(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
		return true
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return true
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return true
	}
	for _, replace := range file.Replace {
		if strings.HasPrefix(replace.New.Path, ".") || strings.HasPrefix(replace.New.Path, "/") {
			return true
		}
	}
	return false
}
