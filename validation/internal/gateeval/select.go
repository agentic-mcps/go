package gateeval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Protocol defaults for SelectOptions.
const (
	defaultPerRepoTrue        = 30
	defaultPerRepoFlaw        = 10
	defaultPerRepoDestructive = 20
	defaultMaxScan            = 3000
	defaultTestTimeout        = 120 * time.Second
	maxChangedLines           = 400
)

// SelectOptions configures Select.
//
//nolint:govet // Keep option fields grouped by meaning.
type SelectOptions struct {
	CorpusCSV, WorkDir, Out, ExclusionsOut string
	PerRepoTrue, PerRepoFlaw               int // 30, 10
	PerRepoDestructive, MaxScan            int // 20, 3000
	TestTimeout                            time.Duration
}

// change is one file of a commit's diff against its parent.
type change struct {
	Status  string // A, M, D, or T
	Path    string
	Added   int
	Deleted int
}

// commitInfo is a commit that passed a static filter.
type commitInfo struct {
	SHA, Base, Rank string
	Direct          []string // slash-form directories of modified non-test .go files
	Lines           int
}

// Select mines commits from the corpus history per the protocol and writes
// one Selection per commit to options.Out and every exclusion to
// options.ExclusionsOut. Both files are replaced.
func Select(ctx context.Context, options SelectOptions) error {
	options = withSelectDefaults(options)
	if options.CorpusCSV == "" || options.Out == "" || options.ExclusionsOut == "" {
		return errors.New("corpus, output and exclusions paths are required")
	}
	workDir, err := absWorkDir(options.WorkDir)
	if err != nil {
		return err
	}
	options.WorkDir = workDir
	projects, err := LoadCorpus(options.CorpusCSV)
	if err != nil {
		return err
	}
	for _, name := range []string{options.Out, options.ExclusionsOut} {
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			return fmt.Errorf("creating %s: %w", name, err)
		}
	}
	for _, project := range projects {
		if err := selectProject(ctx, options, project); err != nil {
			return fmt.Errorf("selecting %s: %w", project.Name, err)
		}
	}
	return nil
}

func withSelectDefaults(options SelectOptions) SelectOptions {
	if options.PerRepoTrue <= 0 {
		options.PerRepoTrue = defaultPerRepoTrue
	}
	if options.PerRepoFlaw <= 0 {
		options.PerRepoFlaw = defaultPerRepoFlaw
	}
	if options.PerRepoDestructive <= 0 {
		options.PerRepoDestructive = defaultPerRepoDestructive
	}
	if options.MaxScan <= 0 {
		options.MaxScan = defaultMaxScan
	}
	if options.TestTimeout <= 0 {
		options.TestTimeout = defaultTestTimeout
	}
	return options
}

// verdict is the outcome of the expensive filter for one commit: a reason
// ("" when it passed) and the direct package directories that can be built.
type verdict struct {
	reason string
	direct []string
}

// selector holds the state of one project's selection.
//
//nolint:govet // Keep fields grouped by meaning.
type selector struct {
	checked map[string]verdict
	options SelectOptions
	project Project
	clone   string
	tree    string
}

func selectProject(ctx context.Context, options SelectOptions, project Project) error {
	clone, err := ensureClone(ctx, options.WorkDir, project)
	if err != nil {
		return err
	}
	tree, err := scratchWorktree(ctx, options.WorkDir, "select", project.Name)
	if err != nil {
		return err
	}
	s := &selector{options: options, project: project, clone: clone, tree: tree, checked: map[string]verdict{}}
	scanned, mainList, destList, err := s.staticPass(ctx)
	if err != nil {
		return err
	}
	exclusions := []Exclusion{
		{Project: project.Name, Reason: fmt.Sprintf("static: %d scanned, %d kept", scanned, len(mainList))},
		{Project: project.Name, Class: ClassDestructive, Reason: fmt.Sprintf("static: %d scanned, %d kept", scanned, len(destList))},
	}
	inMain := map[string]bool{}
	for _, info := range mainList {
		inMain[info.SHA] = true
	}
	trueSet, err := s.take(ctx, mainList, options.PerRepoTrue)
	if err != nil {
		return err
	}
	destSet, err := s.take(ctx, destList, options.PerRepoDestructive)
	if err != nil {
		return err
	}
	for _, sha := range s.excludedOrder(mainList, destList) {
		exclusion := Exclusion{Project: project.Name, Commit: sha, Reason: s.checked[sha].reason}
		if !inMain[sha] {
			exclusion.Class = ClassDestructive
		}
		exclusions = append(exclusions, exclusion)
	}
	for _, selection := range buildSelections(project.Name, trueSet, destSet, options.PerRepoFlaw) {
		if err := AppendJSONL(options.Out, selection); err != nil {
			return err
		}
	}
	for _, exclusion := range exclusions {
		if err := AppendJSONL(options.ExclusionsOut, exclusion); err != nil {
			return err
		}
	}
	return nil
}

// buildSelections combines the strata into one Selection per commit, in rank
// order. The first flaw commits of the true-patch stratum are the flaw seeds.
func buildSelections(project string, trueSet, destSet []commitInfo, flaw int) []Selection {
	bySHA := map[string]*Selection{}
	add := func(info commitInfo) *Selection {
		if selection, ok := bySHA[info.SHA]; ok {
			return selection
		}
		selection := &Selection{
			Project: project, Commit: info.SHA, Base: info.Base, Rank: info.Rank,
			DirectPackages: patternsOf(info.Direct), ChangedLines: info.Lines,
		}
		bySHA[info.SHA] = selection
		return selection
	}
	for n, info := range trueSet {
		selection := add(info)
		selection.TruePatch = true
		selection.FlawSeed = n < flaw
	}
	for _, info := range destSet {
		add(info).Destructive = true
	}
	out := make([]Selection, 0, len(bySHA))
	for _, selection := range bySHA {
		out = append(out, *selection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

// patternsOf turns slash directories into go package patterns.
func patternsOf(dirs []string) []string {
	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "." {
			patterns = append(patterns, ".")
			continue
		}
		patterns = append(patterns, "./"+dir)
	}
	return patterns
}

// staticPass lists the candidate commits and applies both static filters. The
// returned lists are in rank order.
func (s *selector) staticPass(ctx context.Context) (scanned int, mainList, destList []commitInfo, err error) {
	out, err := git(ctx, s.clone, "log", "--first-parent", "--no-merges", "-n", strconv.Itoa(s.options.MaxScan), "--format=%H %P", s.project.Pinned)
	if err != nil {
		return 0, nil, nil, err
	}
	mainList, destList = []commitInfo{}, []commitInfo{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		scanned++
		if len(fields) < 2 {
			continue // root commit: no base to compare with
		}
		info, isMain, isDest, err := s.staticCommit(ctx, fields[0], fields[1])
		if err != nil {
			return 0, nil, nil, err
		}
		if isMain {
			mainList = append(mainList, info)
		}
		if isDest {
			destList = append(destList, info)
		}
	}
	byRank := func(list []commitInfo) {
		sort.Slice(list, func(i, j int) bool { return list[i].Rank < list[j].Rank })
	}
	byRank(mainList)
	byRank(destList)
	return scanned, mainList, destList, nil
}

// rankOf is the deterministic selection order key.
func rankOf(project, sha string) string {
	sum := sha256.Sum256([]byte(project + ":" + sha))
	return hex.EncodeToString(sum[:])
}

// staticCommit applies the cheap filters to one commit.
func (s *selector) staticCommit(ctx context.Context, sha, base string) (info commitInfo, isMain, isDest bool, err error) {
	changes, err := readChanges(ctx, s.clone, base, sha)
	if err != nil {
		return info, false, false, err
	}
	generated := func(file string) bool {
		src, showErr := git(ctx, s.clone, "show", sha+":"+file)
		return showErr == nil && isGeneratedSource(src)
	}
	direct, lines, isMain := staticMain(changes, generated)
	info = commitInfo{SHA: sha, Base: base, Rank: rankOf(s.project.Name, sha), Direct: direct, Lines: lines}
	if !destructiveCandidate(changes, generated) {
		return info, isMain, false, nil
	}
	signal, err := s.destructiveSignal(ctx, base, sha, changes)
	if err != nil {
		return info, false, false, err
	}
	return info, isMain, signal, nil
}

// destructiveSignal reports whether the commit's test or data changes weaken
// the suite: a removed test function, an added skip, a net loss of assertion
// lines, or a touched testdata or golden file.
func (s *selector) destructiveSignal(ctx context.Context, base, sha string, changes []change) (bool, error) {
	if touchesTestData(changes) {
		return true, nil
	}
	files, err := diffFiles(ctx, s.clone, base, sha, "*_test.go")
	if err != nil {
		return false, err
	}
	return weakensTests(files), nil
}

var (
	removedTestFunc = regexp.MustCompile(`^\s*func\s+(Test|Fuzz)`)
	addedSkip       = regexp.MustCompile(`\.(Skip|SkipNow|Skipf)\(`)
	assertionLine   = regexp.MustCompile(`t\.(Error|Fatal)|assert\.|require\.`)
	generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	hunkHeader      = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
)

// weakensTests applies the destructive signals to the diff of _test.go files.
func weakensTests(files []fileDiff) bool {
	net := 0
	for _, file := range files {
		for _, line := range file.Removed {
			if removedTestFunc.MatchString(line) {
				return true
			}
			if assertionLine.MatchString(line) {
				net++
			}
		}
		for _, line := range file.Added {
			if addedSkip.MatchString(line) {
				return true
			}
			if assertionLine.MatchString(line) {
				net--
			}
		}
	}
	return net > 0
}

// fileDiff is one file of a zero-context diff: its path, the removed and
// added line texts, and the current-side line numbers of the added lines.
type fileDiff struct {
	Lines   map[int]bool
	Path    string
	Added   []string
	Removed []string
}

// diffFiles runs a zero-context diff between base and rev, limited to the
// optional pathspecs, and parses it.
func diffFiles(ctx context.Context, dir, base, rev string, pathspecs ...string) ([]fileDiff, error) {
	args := []string{
		"-c", "core.quotepath=false", "diff", "-U0", "--no-color", "--no-ext-diff", "--no-renames",
		"--src-prefix=a/", "--dst-prefix=b/", base, rev,
	}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	out, err := git(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return parseDiff(out), nil
}

// parseDiff parses `git diff -U0` output. Binary files yield no lines.
func parseDiff(out []byte) []fileDiff {
	files := []fileDiff{}
	inHunk := false
	newLine := 0
	oldPath := ""
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, fileDiff{Lines: map[int]bool{}})
			inHunk = false
			oldPath = ""
		case len(files) == 0:
		case !inHunk && strings.HasPrefix(line, "--- "):
			oldPath = diffPath(line[4:])
		case !inHunk && strings.HasPrefix(line, "+++ "):
			target := diffPath(line[4:])
			if target == "" {
				target = oldPath
			}
			files[len(files)-1].Path = target
		case strings.HasPrefix(line, "@@"):
			match := hunkHeader.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			inHunk = true
			newLine, _ = strconv.Atoi(match[1])
			if match[2] == "0" {
				// A pure deletion reports the line before the gap; nothing is added.
				newLine++
			}
		case inHunk && strings.HasPrefix(line, "+"):
			file := &files[len(files)-1]
			file.Added = append(file.Added, line[1:])
			file.Lines[newLine] = true
			newLine++
		case inHunk && strings.HasPrefix(line, "-"):
			file := &files[len(files)-1]
			file.Removed = append(file.Removed, line[1:])
		}
	}
	return files
}

// diffPath strips the a/ or b/ prefix and any trailing tab from a ---/+++
// header path. /dev/null yields "".
func diffPath(header string) string {
	header, _, _ = strings.Cut(header, "\t")
	if header == "/dev/null" {
		return ""
	}
	if len(header) > 2 && (header[:2] == "a/" || header[:2] == "b/") {
		return header[2:]
	}
	return header
}

// isTestFile reports whether the path is a Go test file.
func isTestFile(p string) bool { return strings.HasSuffix(p, "_test.go") }

// skippedPath reports whether the path is under vendor/ or testdata/.
func skippedPath(p string) bool {
	for _, element := range strings.Split(p, "/") {
		if element == "vendor" || element == "testdata" {
			return true
		}
	}
	return false
}

// touchesTestData reports whether any changed path is testdata or a golden file.
func touchesTestData(changes []change) bool {
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".golden") {
			return true
		}
		for _, element := range strings.Split(c.Path, "/") {
			if element == "testdata" {
				return true
			}
		}
	}
	return false
}

// touchesModule reports a go.mod or go.sum change at any depth.
func touchesModule(changes []change) bool {
	for _, c := range changes {
		if base := path.Base(c.Path); base == "go.mod" || base == "go.sum" {
			return true
		}
	}
	return false
}

// totalLines sums added and deleted lines of the diff.
func totalLines(changes []change) int {
	total := 0
	for _, c := range changes {
		total += c.Added + c.Deleted
	}
	return total
}

// staticMain applies the cheap rules of the main filter. direct lists the
// directories of modified non-test .go files (generated ones included, since
// the package changed); ok reports a qualifying commit.
func staticMain(changes []change, generated func(string) bool) (direct []string, lines int, ok bool) {
	lines = totalLines(changes)
	modified, tested, seen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range changes {
		if !strings.HasSuffix(c.Path, ".go") || skippedPath(c.Path) {
			continue
		}
		dir := path.Dir(c.Path)
		switch {
		case isTestFile(c.Path):
			if c.Status == "A" || c.Status == "M" {
				tested[dir] = true
			}
		case c.Status == "M":
			if !seen[dir] {
				seen[dir] = true
				direct = append(direct, dir)
			}
			if !modified[dir] && !generated(c.Path) {
				modified[dir] = true
			}
		}
	}
	sort.Strings(direct)
	if touchesModule(changes) || lines > maxChangedLines {
		return direct, lines, false
	}
	for dir := range modified {
		if tested[dir] {
			return direct, lines, true
		}
	}
	return direct, lines, false
}

// destructiveCandidate applies the size, module and non-test rules of the
// destructive filter: no go.mod/go.sum change, at most 400 lines, and at
// least one modified non-test .go file that is not generated.
func destructiveCandidate(changes []change, generated func(string) bool) bool {
	if touchesModule(changes) || totalLines(changes) > maxChangedLines {
		return false
	}
	for _, c := range changes {
		if c.Status == "M" && strings.HasSuffix(c.Path, ".go") && !isTestFile(c.Path) && !skippedPath(c.Path) && !generated(c.Path) {
			return true
		}
	}
	return false
}

// isGeneratedSource reports a "Code generated ... DO NOT EDIT." line before
// the package clause.
func isGeneratedSource(src []byte) bool {
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimRight(line, "\r")
		if generatedHeader.MatchString(line) {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// readChanges lists the files changed between base and sha with their status
// and line counts. Renames are reported as a delete and an add.
func readChanges(ctx context.Context, dir, base, sha string) ([]change, error) {
	status, err := git(ctx, dir, "-c", "core.quotepath=false", "diff", "--name-status", "--no-renames", "--no-ext-diff", "-z", base, sha)
	if err != nil {
		return nil, err
	}
	numstat, err := git(ctx, dir, "-c", "core.quotepath=false", "diff", "--numstat", "--no-renames", "--no-ext-diff", "-z", base, sha)
	if err != nil {
		return nil, err
	}
	return parseChanges(status, numstat), nil
}

// parseChanges merges `git diff --name-status -z` and `--numstat -z` output.
// A binary file counts as zero lines.
func parseChanges(status, numstat []byte) []change {
	counts := map[string][2]int{}
	for _, entry := range bytes.Split(numstat, []byte{0}) {
		fields := strings.SplitN(string(entry), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		deleted, _ := strconv.Atoi(fields[1])
		counts[fields[2]] = [2]int{added, deleted}
	}
	parts := strings.Split(string(status), "\x00")
	changes := []change{}
	for i := 0; i+1 < len(parts); i += 2 {
		c := counts[parts[i+1]]
		changes = append(changes, change{Status: parts[i][:1], Path: parts[i+1], Added: c[0], Deleted: c[1]})
	}
	return changes
}

// take applies the expensive filter to list in rank order until quota
// commits pass, and returns those commits.
func (s *selector) take(ctx context.Context, list []commitInfo, quota int) ([]commitInfo, error) {
	taken := []commitInfo{}
	for _, info := range list {
		if len(taken) >= quota {
			break
		}
		result, ok := s.checked[info.SHA]
		if !ok {
			var err error
			if result, err = s.expensive(ctx, info); err != nil {
				return nil, err
			}
			s.checked[info.SHA] = result
		}
		if result.reason == "" {
			info.Direct = result.direct
			taken = append(taken, info)
		}
	}
	return taken, nil
}

// excludedOrder lists the commits that failed the expensive filter, each once,
// in the order the lists give them.
func (s *selector) excludedOrder(lists ...[]commitInfo) []string {
	var shas []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, info := range list {
			if result, ok := s.checked[info.SHA]; ok && result.reason != "" && !seen[info.SHA] {
				seen[info.SHA] = true
				shas = append(shas, info.SHA)
			}
		}
	}
	return shas
}

// expensive builds and tests the commit and its base. It returns the reason
// the commit fails, or "" when it passes. Only cancellation is an error.
func (s *selector) expensive(ctx context.Context, info commitInfo) (verdict, error) {
	if err := checkout(ctx, s.tree, info.SHA); err != nil {
		return verdict{}, err
	}
	direct := make([]string, 0, len(info.Direct))
	for _, dir := range info.Direct {
		if buildableDir(s.tree, dir) {
			direct = append(direct, dir)
		}
	}
	if len(direct) == 0 {
		return verdict{reason: "no buildable direct package"}, nil
	}
	test := append([]string{"test", "-count=1"}, patternsOf(direct)...)
	steps := []struct {
		rev, reason string
		args        []string
		repeat      int
	}{
		{info.Base, "build fails at base", []string{"build", "./..."}, 1},
		{info.Base, "tests fail at base", test, 1},
		{info.SHA, "build fails at commit", []string{"build", "./..."}, 1},
		{info.SHA, "tests fail or are flaky at commit", test, 2},
	}
	current := info.SHA
	for _, step := range steps {
		if step.rev != current {
			if err := checkout(ctx, s.tree, step.rev); err != nil {
				return verdict{}, err
			}
			current = step.rev
		}
		for range step.repeat {
			result := runCommand(ctx, s.tree, s.options.TestTimeout, "go", step.args...)
			if err := ctx.Err(); err != nil {
				return verdict{}, fmt.Errorf("filtering %s: %w", info.SHA, err)
			}
			if result.err != nil || result.exit != 0 {
				return verdict{reason: step.reason}, nil
			}
		}
	}
	return verdict{direct: direct}, nil
}
