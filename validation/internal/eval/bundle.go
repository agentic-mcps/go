package eval

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BundleRecord identifies one deterministic private fixture bundle.
type BundleRecord struct {
	TaskID         string `json:"task_id"`
	Repository     string `json:"repository"`
	UpstreamBase   string `json:"upstream_base"`
	UpstreamTarget string `json:"upstream_target"`
	FixtureBase    string `json:"fixture_base"`
	FixtureTarget  string `json:"fixture_target"`
	Bundle         string `json:"bundle"`
	SHA256         string `json:"sha256"`
	GitVersion     string `json:"git_version"`
}

// PrepareAll prepares deterministic bundles for every supplied task.
func PrepareAll(ctx context.Context, tasks []Task, sourceRoot, outputRoot string) ([]BundleRecord, error) {
	if sourceRoot == "" || outputRoot == "" {
		return nil, errors.New("source and output roots are required")
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return nil, err
	}
	records := make([]BundleRecord, 0, len(tasks))
	for _, task := range tasks {
		record, err := Prepare(ctx, task, filepath.Join(sourceRoot, task.Repository.Name), outputRoot)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", task.ID, err)
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].TaskID < records[j].TaskID })
	return records, nil
}

// Prepare builds one deterministic two-commit fixture bundle.
func Prepare(ctx context.Context, task Task, source, outputRoot string) (BundleRecord, error) {
	var record BundleRecord
	root, evalErr := filepath.EvalSymlinks(source)
	if evalErr != nil {
		return record, evalErr
	}
	gotRoot, err := commandText(ctx, "", nil, "git", "-C", root, "rev-parse", "--show-toplevel")
	if err != nil || gotRoot != root {
		return record, fmt.Errorf("source is not the repository root: got %q: %w", gotRoot, err)
	}
	parent, err := commandText(ctx, "", nil, "git", "-C", root, "rev-parse", task.Repository.Target+"^")
	if err != nil {
		return record, fmt.Errorf("resolve target parent: %w", err)
	}
	if parent != task.Repository.Base {
		return record, fmt.Errorf("target parent is %s, manifest requires %s", parent, task.Repository.Base)
	}
	for _, commit := range []string{task.Repository.Base, task.Repository.Target} {
		kind, verifyErr := commandText(ctx, "", nil, "git", "-C", root, "cat-file", "-t", commit)
		if verifyErr != nil || kind != "commit" {
			return record, fmt.Errorf("required commit %s is unavailable", commit)
		}
	}
	tmp, err := os.MkdirTemp("", "agentic-go-eval-bundle-")
	if err != nil {
		return record, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	fixture := filepath.Join(tmp, "fixture")
	if mkdirErr := os.Mkdir(fixture, 0o755); mkdirErr != nil {
		return record, mkdirErr
	}
	if _, initErr := commandText(ctx, fixture, nil, "git", "init", "--quiet", "--initial-branch=base"); initErr != nil {
		return record, initErr
	}
	if materializeErr := materializeArchive(ctx, root, task.Repository.Base, fixture); materializeErr != nil {
		return record, fmt.Errorf("materialize base: %w", materializeErr)
	}
	baseMessage := "fixture: base snapshot\n\nUpstream-Commit: " + task.Repository.Base
	baseFixture, err := fixtureCommit(ctx, fixture, baseMessage, "2000-01-01T00:00:01Z")
	if err != nil {
		return record, err
	}
	if _, switchErr := commandText(ctx, fixture, nil, "git", "switch", "--quiet", "-c", "target"); switchErr != nil {
		return record, switchErr
	}
	if clearErr := clearFixture(fixture); clearErr != nil {
		return record, clearErr
	}
	if materializeErr := materializeArchive(ctx, root, task.Repository.Target, fixture); materializeErr != nil {
		return record, fmt.Errorf("materialize target: %w", materializeErr)
	}
	targetMessage := "fixture: target snapshot\n\nUpstream-Commit: " + task.Repository.Target
	targetFixture, err := fixtureCommit(ctx, fixture, targetMessage, "2000-01-01T00:00:02Z")
	if err != nil {
		return record, err
	}
	bundleTmp := filepath.Join(tmp, task.ID+".bundle")
	if _, bundleErr := commandText(ctx, fixture, nil, "git", "-c", "pack.threads=1", "-c", "pack.window=0", "-c", "pack.depth=0", "bundle", "create", bundleTmp, "refs/heads/base", "refs/heads/target"); bundleErr != nil {
		return record, fmt.Errorf("create bundle: %w", bundleErr)
	}
	digest, err := fileSHA256(bundleTmp)
	if err != nil {
		return record, err
	}
	name := task.ID + "-" + digest + ".bundle"
	final := filepath.Join(outputRoot, name)
	if installErr := installImmutable(bundleTmp, final); installErr != nil {
		return record, installErr
	}
	gitVersion, _ := commandText(ctx, "", nil, "git", "version")
	record = BundleRecord{
		TaskID: task.ID, Repository: task.Repository.URL,
		UpstreamBase: task.Repository.Base, UpstreamTarget: task.Repository.Target,
		FixtureBase: baseFixture, FixtureTarget: targetFixture,
		Bundle: name, SHA256: digest, GitVersion: gitVersion,
	}
	metadata, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return record, err
	}
	metadata = append(metadata, '\n')
	if err := atomicWrite(filepath.Join(outputRoot, task.ID+".json"), metadata, 0o644); err != nil {
		return record, err
	}
	return record, nil
}

// Setup clones one fixture base into a new candidate workspace.
func Setup(ctx context.Context, task Task, bundle, workspace string) error {
	if _, err := os.Stat(workspace); err == nil {
		return fmt.Errorf("workspace already exists: %s", workspace)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := commandText(ctx, "", nil, "git", "clone", "--quiet", "--branch", "base", "--single-branch", "--", bundle, workspace); err != nil {
		return err
	}
	if _, err := commandText(ctx, workspace, nil, "git", "remote", "remove", "origin"); err != nil {
		return err
	}
	message, err := commandText(ctx, workspace, nil, "git", "show", "-s", "--format=%B", "HEAD")
	if err != nil || !strings.Contains(message, "Upstream-Commit: "+task.Repository.Base) {
		return errors.New("bundle base provenance does not match task manifest")
	}
	parents, err := commandText(ctx, workspace, nil, "git", "show", "-s", "--format=%P", "HEAD")
	if err != nil || parents != "" {
		return errors.New("fixture base must be a root commit")
	}
	return nil
}

func fixtureCommit(ctx context.Context, root, message, date string) (string, error) {
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Agentic Go Evaluation", "GIT_AUTHOR_EMAIL=evaluation@invalid",
		"GIT_COMMITTER_NAME=Agentic Go Evaluation", "GIT_COMMITTER_EMAIL=evaluation@invalid",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
	)
	if _, err := commandText(ctx, root, env, "git", "add", "-A"); err != nil {
		return "", err
	}
	if _, err := commandText(ctx, root, env, "git", "commit", "--quiet", "--no-gpg-sign", "-m", message); err != nil {
		return "", err
	}
	return commandText(ctx, root, nil, "git", "rev-parse", "HEAD")
}

func materializeArchive(ctx context.Context, source, commit, destination string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", source, "archive", "--format=tar", commit)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if startErr := cmd.Start(); startErr != nil {
		return startErr
	}
	tarReader := tar.NewReader(stdout)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = cmd.Process.Kill()
			return nextErr
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if validateErr := validateRelativePath(name); validateErr != nil {
			_ = cmd.Process.Kill()
			return validateErr
		}
		path := filepath.Join(destination, name)
		if containmentErr := ensureContained(destination, path); containmentErr != nil {
			_ = cmd.Process.Kill()
			return containmentErr
		}
		switch header.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			err = os.MkdirAll(path, 0o755)
		case tar.TypeReg:
			err = writeArchiveFile(path, tarReader, os.FileMode(header.Mode)&0o777)
		case tar.TypeSymlink:
			target := filepath.Clean(filepath.FromSlash(header.Linkname))
			if filepath.IsAbs(target) || target == ".." || strings.HasPrefix(target, "../") {
				err = fmt.Errorf("unsafe archive symlink %s -> %s", name, target)
			} else {
				err = os.MkdirAll(filepath.Dir(path), 0o755)
				if err == nil {
					err = os.Symlink(target, path)
				}
			}
		default:
			err = fmt.Errorf("unsupported archive entry %s type %d", name, header.Typeflag)
		}
		if err != nil {
			_ = cmd.Process.Kill()
			return err
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func writeArchiveFile(path string, source io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, source)
	closeErr := f.Close()
	return errors.Join(copyErr, closeErr)
}

func clearFixture(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func installImmutable(source, destination string) error {
	if current, err := fileSHA256(destination); err == nil {
		incoming, digestErr := fileSHA256(source)
		if digestErr != nil {
			return digestErr
		}
		if current != incoming {
			return fmt.Errorf("existing bundle digest differs: %s", destination)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func commandText(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return text, nil
}

func ensureContained(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes destination: %s", path)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
