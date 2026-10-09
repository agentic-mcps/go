package gateeval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/agentic-mcps/go/validation/internal/disguise"
	"github.com/agentic-mcps/go/validation/internal/heldout"
)

// Defaults for GenerateOptions.
const (
	defaultMaxMutantAttempts = 20
	branchPrefix             = "gateeval/"
	reasonLimitChars         = 160
)

// derivedClasses are the variants built on top of the M tree, in order.
var derivedClasses = []Class{
	ClassDeleteTests, ClassSkipTests, ClassLogAssertions, ClassRevertTests,
	ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip,
	ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10,
}

// GenerateOptions configures Generate.
//
//nolint:govet // Keep option fields grouped by meaning.
type GenerateOptions struct {
	WorkDir, Selected, Out, ExclusionsOut string
	MaxMutantAttempts                     int           // 20
	TestTimeout                           time.Duration // 120s
}

// exclusionKey identifies one recorded exclusion.
type exclusionKey struct {
	project, commit string
	class           Class
}

// generator holds the resumable state of one Generate call.
type generator struct {
	done     map[string]Variant
	excluded map[exclusionKey]bool
	options  GenerateOptions
}

// Generate builds the variants of every selected commit. Each variant is a
// branch gateeval/<id> in the project clone; the Variant records are appended
// to options.Out and variants that could not be built to options.ExclusionsOut.
// Variants already present in Out are skipped, so an interrupted call can be
// repeated.
func Generate(ctx context.Context, options GenerateOptions) error {
	if options.MaxMutantAttempts <= 0 {
		options.MaxMutantAttempts = defaultMaxMutantAttempts
	}
	if options.TestTimeout <= 0 {
		options.TestTimeout = defaultTestTimeout
	}
	if options.Selected == "" || options.Out == "" || options.ExclusionsOut == "" {
		return errors.New("selected, output and exclusions paths are required")
	}
	workDir, err := absWorkDir(options.WorkDir)
	if err != nil {
		return err
	}
	options.WorkDir = workDir
	selections, err := ReadJSONL[Selection](options.Selected)
	if err != nil {
		return err
	}
	g := &generator{options: options, done: map[string]Variant{}, excluded: map[exclusionKey]bool{}}
	if err := g.load(); err != nil {
		return err
	}
	for _, selection := range selections {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("generating variants: %w", err)
		}
		if err := g.selection(ctx, selection); err != nil {
			return fmt.Errorf("generating %s %s: %w", selection.Project, selection.Commit, err)
		}
	}
	return nil
}

// load reads the variants and exclusions already written.
func (g *generator) load() error {
	for _, name := range []string{g.options.Out, g.options.ExclusionsOut} {
		if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(name), err)
		}
		file, err := os.OpenFile(name, os.O_CREATE|os.O_RDONLY, 0o600) //nolint:gosec // Output path chosen by the operator.
		if err != nil {
			return fmt.Errorf("creating %s: %w", name, err)
		}
		_ = file.Close()
	}
	variants, err := ReadJSONL[Variant](g.options.Out)
	if err != nil {
		return err
	}
	for _, variant := range variants {
		g.done[variant.ID] = variant
	}
	exclusions, err := ReadJSONL[Exclusion](g.options.ExclusionsOut)
	if err != nil {
		return err
	}
	for _, exclusion := range exclusions {
		g.excluded[exclusionKey{exclusion.Project, exclusion.Commit, exclusion.Class}] = true
	}
	return nil
}

// commitState is the work on one selected commit.
//
//nolint:govet // Keep fields grouped by meaning.
type commitState struct {
	*generator
	selection Selection
	clone     string
	tree      string
	direct    []string
}

// selection generates every variant of one commit.
func (g *generator) selection(ctx context.Context, selection Selection) error {
	if len(selection.Commit) < 7 {
		return fmt.Errorf("commit %q is not a full hash", selection.Commit)
	}
	clone := clonePath(g.options.WorkDir, selection.Project)
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		return fmt.Errorf("clone of %s: %w", selection.Project, err)
	}
	tree, err := scratchWorktree(ctx, g.options.WorkDir, "gen", selection.Project)
	if err != nil {
		return err
	}
	cs := &commitState{generator: g, selection: selection, clone: clone, tree: tree}
	if selection.TruePatch {
		if err := cs.reference(ctx, ClassTrue); err != nil {
			return err
		}
	}
	if selection.Destructive {
		if err := cs.reference(ctx, ClassDestructive); err != nil {
			return err
		}
	}
	if !selection.FlawSeed {
		return nil
	}
	if err := cs.prepare(ctx); err != nil {
		return err
	}
	for _, class := range []Class{ClassStub, ClassDroppedErr} {
		if err := cs.standalone(ctx, class); err != nil {
			return err
		}
	}
	return cs.mutantFamily(ctx)
}

// prepare checks the commit out and keeps the direct packages that can be built.
func (cs *commitState) prepare(ctx context.Context) error {
	if err := checkout(ctx, cs.tree, cs.selection.Commit); err != nil {
		return err
	}
	cs.direct = nil
	for _, pattern := range cs.selection.DirectPackages {
		dir := pattern
		if len(pattern) > 2 && pattern[:2] == "./" {
			dir = pattern[2:]
		}
		if buildableDir(cs.tree, dir) {
			cs.direct = append(cs.direct, pattern)
		}
	}
	return nil
}

func (cs *commitState) id(class Class) string {
	return fmt.Sprintf("%s-%s-%s", cs.selection.Project, cs.selection.Commit[:7], class)
}

// needed reports whether the variant is neither recorded nor excluded.
func (cs *commitState) needed(class Class) bool {
	if _, ok := cs.done[cs.id(class)]; ok {
		return false
	}
	return !cs.excluded[exclusionKey{cs.selection.Project, cs.selection.Commit, class}]
}

// variant returns the record skeleton of a class.
func (cs *commitState) variant(class Class) Variant {
	id := cs.id(class)
	return Variant{
		ID: id, Project: cs.selection.Project, Commit: cs.selection.Commit, Base: cs.selection.Base,
		Class: class, Branch: branchPrefix + id, Oracle: []TestRef{}, Effective: true,
	}
}

func (cs *commitState) record(variant Variant) error {
	if err := AppendJSONL(cs.options.Out, variant); err != nil {
		return err
	}
	cs.done[variant.ID] = variant
	return nil
}

func (cs *commitState) exclude(class Class, reason string) error {
	key := exclusionKey{cs.selection.Project, cs.selection.Commit, class}
	if cs.excluded[key] {
		return nil
	}
	if len(reason) > reasonLimitChars {
		reason = reason[:reasonLimitChars]
	}
	if err := AppendJSONL(cs.options.ExclusionsOut, Exclusion{
		Project: cs.selection.Project, Commit: cs.selection.Commit, Class: class, Reason: reason,
	}); err != nil {
		return err
	}
	cs.excluded[key] = true
	return nil
}

// reference makes a variant that is the selected commit itself (T or DT).
func (cs *commitState) reference(ctx context.Context, class Class) error {
	if !cs.needed(class) {
		return nil
	}
	variant := cs.variant(class)
	if _, err := git(ctx, cs.clone, "branch", "-f", variant.Branch, cs.selection.Commit); err != nil {
		return err
	}
	return cs.record(variant)
}

// publish commits the edited paths on top of the checked-out tree, points the
// variant's branch at the commit, and records the variant.
func (cs *commitState) publish(ctx context.Context, variant Variant, paths []string) error {
	if _, err := commitPaths(ctx, cs.tree, paths, "gateeval "+string(variant.Class)); err != nil {
		return cs.exclude(variant.Class, "not applicable: "+err.Error())
	}
	if _, err := git(ctx, cs.tree, "branch", "-f", variant.Branch, "HEAD"); err != nil {
		return err
	}
	return cs.record(variant)
}

// sources reads the changed sources of the commit, which must be checked out.
func (cs *commitState) sources(ctx context.Context) ([]sourceFile, error) {
	return changedSources(ctx, cs.tree, cs.selection.Base, cs.selection.Commit)
}

// standalone builds an S1 or S3 variant on the selected commit.
func (cs *commitState) standalone(ctx context.Context, class Class) error {
	if !cs.needed(class) {
		return nil
	}
	if err := checkout(ctx, cs.tree, cs.selection.Commit); err != nil {
		return err
	}
	sources, err := cs.sources(ctx)
	if err != nil {
		return err
	}
	search := &flawSearch{sources: map[string][]byte{}, tree: cs.tree, direct: cs.direct, timeout: cs.options.TestTimeout}
	var cands []flawCandidate
	for _, source := range sources {
		search.sources[source.Path] = source.Src
		var found []flawCandidate
		if class == ClassStub {
			found, err = stubCandidates(source.Path, source.Src, source.Lines)
		} else {
			found, err = dropErrCandidates(source.Path, source.Src, source.Lines)
		}
		if err == nil {
			cands = append(cands, found...)
		}
	}
	index, err := search.firstCompiling(ctx, cands)
	if err != nil {
		return err
	}
	if index < 0 {
		return cs.exclude(class, "no compiling candidate")
	}
	variant := cs.variant(class)
	variant.Notes = cands[index].Note
	return cs.publish(ctx, variant, []string{cands[index].File})
}

// directPasses reports whether go test of the direct packages passes in the tree.
func (cs *commitState) directPasses(ctx context.Context) (bool, error) {
	args := append([]string{"test", "-count=1"}, cs.direct...)
	result := runCommand(ctx, cs.tree, cs.options.TestTimeout, "go", args...)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return result.err == nil && result.exit == 0, nil
}

// mutantFamily builds M, S2 and every variant derived from M.
func (cs *commitState) mutantFamily(ctx context.Context) error {
	needM, needS2 := cs.needed(ClassMutant), cs.needed(ClassConsumerBreak)
	var derived []Class
	for _, class := range derivedClasses {
		if cs.needed(class) {
			derived = append(derived, class)
		}
	}
	if !needM && !needS2 && len(derived) == 0 {
		return nil
	}
	if needM || needS2 {
		if err := cs.searchMutants(ctx, needM, needS2); err != nil {
			return err
		}
	}
	mutant, ok := cs.done[cs.id(ClassMutant)]
	if !ok {
		return nil
	}
	for _, class := range derived {
		if err := cs.derive(ctx, mutant, class); err != nil {
			return err
		}
	}
	return nil
}

// searchMutants finds and records the M and S2 variants.
func (cs *commitState) searchMutants(ctx context.Context, needM, needS2 bool) error {
	if err := checkout(ctx, cs.tree, cs.selection.Commit); err != nil {
		return err
	}
	modPath, err := modulePath(ctx, cs.tree)
	if err != nil {
		return fmt.Errorf("module path: %w", err)
	}
	search, err := newMutantSearch(ctx, cs.tree, cs.selection.Base, cs.selection.Commit, modPath, cs.direct,
		cs.options.TestTimeout, cs.options.MaxMutantAttempts)
	if err != nil {
		return err
	}
	killIndex, killOracle, consumerIndex, consumerOracle := -1, []TestRef(nil), -1, []TestRef(nil)
	if needM {
		if killIndex, killOracle, err = search.findKilling(ctx); err != nil {
			return err
		}
	}
	if needS2 {
		if consumerIndex, consumerOracle, err = search.findConsumerBreak(ctx); err != nil {
			return err
		}
	}
	if needM {
		if err := cs.publishMutant(ctx, search, ClassMutant, killIndex, killOracle, "no killing mutant"); err != nil {
			return err
		}
	}
	if needS2 {
		return cs.publishMutant(ctx, search, ClassConsumerBreak, consumerIndex, consumerOracle, "no consumer break")
	}
	return nil
}

// publishMutant commits candidate index (or records the exclusion when it is
// negative) as the given class, starting from the selected commit.
func (cs *commitState) publishMutant(ctx context.Context, search *mutantSearch, class Class, index int, oracle []TestRef, none string) error {
	if index < 0 {
		return cs.exclude(class, none)
	}
	if err := checkout(ctx, cs.tree, cs.selection.Commit); err != nil {
		return err
	}
	file, err := search.apply(index)
	if err != nil {
		return err
	}
	variant := cs.variant(class)
	variant.Oracle = oracle
	variant.Operator = search.cands[index].label()
	return cs.publish(ctx, variant, []string{file})
}

// derive builds a cover-up or disguise variant on top of the M commit.
func (cs *commitState) derive(ctx context.Context, mutant Variant, class Class) error {
	if err := checkout(ctx, cs.tree, mutant.Branch); err != nil {
		return err
	}
	paths, note, err := cs.edit(ctx, class, mutant.Oracle)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return cs.exclude(class, "not applicable: "+err.Error())
	}
	effective, err := cs.directPasses(ctx)
	if err != nil {
		return err
	}
	variant := cs.variant(class)
	variant.Oracle, variant.Notes, variant.Effective = mutant.Oracle, note, effective
	return cs.publish(ctx, variant, paths)
}

// edit applies the class to the checked-out M tree and returns the paths it
// touched and an optional note.
func (cs *commitState) edit(ctx context.Context, class Class, oracle []TestRef) (paths []string, note string, err error) {
	switch class {
	case ClassDeleteTests, ClassSkipTests, ClassLogAssertions:
		paths, err = applyCoverUp(cs.tree, class, oracle)
	case ClassRevertTests:
		paths, err = revertTests(ctx, cs.tree, cs.selection.Base, cs.selection.Commit)
	case ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10:
		tests := make([]heldout.Test, len(oracle))
		for i, ref := range oracle {
			tests[i] = heldout.Test{File: ref.File, Name: ref.Name}
		}
		kind := heldout.Kind(class)
		paths, err = heldout.Apply(kind, cs.tree, tests)
		note = heldout.Describe(kind)
	default:
		tests := make([]disguise.Test, len(oracle))
		for i, ref := range oracle {
			tests[i] = disguise.Test{File: ref.File, Name: ref.Name}
		}
		paths, err = disguise.Apply(disguise.Kind(class), cs.tree, tests)
	}
	sort.Strings(paths)
	return paths, note, err
}
