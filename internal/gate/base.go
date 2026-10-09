package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/execution"
)

// Source labels for Base.Source.
const (
	baseSourceFlag      = "flag"
	baseSourceSession   = "session"
	baseSourceGitHub    = "github"
	baseSourceHead      = "head"
	baseSourceHeadShort = "head (shallow clone; pass --base)"
)

// baseDefaultRefs are the well-known default-branch refs, tried in order.
var baseDefaultRefs = []string{"origin/HEAD", "origin/main", "origin/master", "main", "master"}

// NewGit returns a GitFunc that runs git in root through the shared runner.
func NewGit(runner *execution.Runner, root string) GitFunc {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		result, err := runner.Run(ctx, execution.Command{Name: "git", Args: args, Dir: root}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = fmt.Sprintf("git exited with status %d", result.ExitCode)
			}
			return nil, errors.New(message)
		}
		return stdout.Bytes(), nil
	}
}

// DetectBase chooses what the change is compared against. The first candidate
// that resolves wins: the explicit ref, the session start commit, the pull
// request target, the remote default branch, and finally HEAD itself.
func DetectBase(ctx context.Context, git GitFunc, explicit, sessionBase string, getenv func(string) string) (Base, error) {
	if _, err := baseGitText(ctx, git, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return Base{}, errors.New("repository has no commits")
	}

	if explicit != "" {
		base, err := baseResolveRef(ctx, git, explicit, baseSourceFlag)
		if err != nil {
			return Base{}, fmt.Errorf("resolving base %q: %w", explicit, err)
		}
		return base, nil
	}

	if base, ok := baseFromSession(ctx, git, sessionBase); ok {
		return base, nil
	}

	if getenv != nil {
		if name := getenv("GITHUB_BASE_REF"); name != "" {
			for _, ref := range []string{"origin/" + name, name} {
				if base, err := baseResolveRef(ctx, git, ref, baseSourceGitHub); err == nil {
					return base, nil
				}
			}
		}
	}

	for _, ref := range baseDefaultRefs {
		if base, err := baseResolveRef(ctx, git, ref, ref); err == nil {
			return base, nil
		}
	}

	return baseHead(ctx, git)
}

// baseFromSession accepts the session start commit only while it is still an
// ancestor of HEAD, so a rewritten history falls through to other candidates.
func baseFromSession(ctx context.Context, git GitFunc, sessionBase string) (Base, bool) {
	if sessionBase == "" {
		return Base{}, false
	}
	commit, err := baseResolveCommit(ctx, git, sessionBase)
	if err != nil {
		return Base{}, false
	}
	if _, err := git(ctx, "merge-base", "--is-ancestor", commit, "HEAD"); err != nil {
		return Base{}, false
	}
	return Base{Ref: sessionBase, Commit: commit, Source: baseSourceSession}, true
}

// baseResolveRef resolves ref to a commit and takes its merge-base with HEAD.
func baseResolveRef(ctx context.Context, git GitFunc, ref, source string) (Base, error) {
	if _, err := baseResolveCommit(ctx, git, ref); err != nil {
		return Base{}, err
	}
	commit, err := baseGitText(ctx, git, "merge-base", ref, "HEAD")
	if err != nil {
		return Base{}, fmt.Errorf("finding merge-base of %s and HEAD: %w", ref, err)
	}
	return Base{Ref: ref, Commit: commit, Source: source}, nil
}

func baseResolveCommit(ctx context.Context, git GitFunc, ref string) (string, error) {
	if err := baseCheckRef(ref); err != nil {
		return "", err
	}
	commit, err := baseGitText(ctx, git, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%s does not resolve to a commit: %w", ref, err)
	}
	if commit == "" {
		return "", fmt.Errorf("%s does not resolve to a commit", ref)
	}
	return commit, nil
}

// baseHead is the last resort: compare against HEAD so only uncommitted
// changes are checked.
func baseHead(ctx context.Context, git GitFunc) (Base, error) {
	commit, err := baseResolveCommit(ctx, git, "HEAD")
	if err != nil {
		return Base{}, errors.New("repository has no commits")
	}
	source := baseSourceHead
	if shallow, err := baseGitText(ctx, git, "rev-parse", "--is-shallow-repository"); err == nil && shallow == "true" {
		source = baseSourceHeadShort
	}
	return Base{Ref: "HEAD", Commit: commit, Source: source}, nil
}

// ChangedFiles lists workspace-relative slash paths changed between baseCommit and the
// working tree, including staged, unstaged and untracked (non-ignored) files.
// Deleted files are listed only when includeDeleted is set.
func ChangedFiles(ctx context.Context, git GitFunc, baseCommit string, includeDeleted bool) ([]string, error) {
	if err := baseCheckRef(baseCommit); err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}
	// --no-renames reports a rename as delete plus add so the old path stays
	// visible when deleted files are requested.
	args := []string{"diff", "--relative", "--no-renames", "--name-only", "-z"}
	if !includeDeleted {
		args = append(args, "--diff-filter=ACMR")
	}
	args = append(args, baseCommit, "--", ".")
	tracked, err := git(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}
	untracked, err := git(ctx, "ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("listing untracked files: %w", err)
	}

	seen := make(map[string]struct{})
	files := make([]string, 0)
	for _, output := range [][]byte{tracked, untracked} {
		for _, name := range baseSplitNUL(output) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// ChangedGoFiles is ChangedFiles filtered to .go files.
func ChangedGoFiles(ctx context.Context, git GitFunc, baseCommit string, includeDeleted bool) ([]string, error) {
	files, err := ChangedFiles(ctx, git, baseCommit, includeDeleted)
	if err != nil {
		return nil, err
	}
	goFiles := make([]string, 0, len(files))
	for _, name := range files {
		if strings.HasSuffix(name, ".go") {
			goFiles = append(goFiles, name)
		}
	}
	return goFiles, nil
}

func baseSplitNUL(data []byte) []string {
	fields := bytes.Split(data, []byte{0})
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 0 {
			names = append(names, string(field))
		}
	}
	return names
}

// baseCheckRef rejects refs git would parse as command-line options.
func baseCheckRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q must not start with '-'", ref)
	}
	return nil
}

func baseGitText(ctx context.Context, git GitFunc, args ...string) (string, error) {
	output, err := git(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
