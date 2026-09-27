package retrievalstudy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func sourceIdentity(parent context.Context, sourceRepository string, timeout time.Duration, manifestSHA256 string) (Reproducibility, error) {
	root, err := gitText(parent, timeout, sourceRepository, "rev-parse", "--show-toplevel")
	if err != nil {
		return Reproducibility{}, fmt.Errorf("locating study source repository: %w", err)
	}
	root = strings.TrimSpace(root)
	commit, err := gitText(parent, timeout, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Reproducibility{}, fmt.Errorf("reading study source commit: %w", err)
	}
	dirtyDigest := sha256.New()
	if _, err := dirtyDigest.Write([]byte("agentic-go-dirty-diff/v1\x00")); err != nil {
		return Reproducibility{}, err
	}
	if err := hashGitOutput(parent, timeout, root, dirtyDigest, "diff", "--binary", "HEAD", "--"); err != nil {
		return Reproducibility{}, fmt.Errorf("hashing tracked study-source changes: %w", err)
	}
	if _, err := dirtyDigest.Write([]byte("\x00untracked-files\x00")); err != nil {
		return Reproducibility{}, err
	}
	untracked, err := gitTextBytes(parent, timeout, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Reproducibility{}, fmt.Errorf("listing untracked study-source files: %w", err)
	}
	paths := make([]string, 0)
	for _, rawPath := range strings.Split(string(untracked), "\x00") {
		if rawPath != "" {
			paths = append(paths, rawPath)
		}
	}
	sort.Strings(paths)
	for _, relative := range paths {
		if err := parent.Err(); err != nil {
			return Reproducibility{}, err
		}
		if !filepath.IsLocal(filepath.FromSlash(relative)) || path.Clean(relative) != relative {
			return Reproducibility{}, fmt.Errorf("untracked study-source path is unsafe")
		}
		if err := writeField(dirtyDigest, []byte(relative)); err != nil {
			return Reproducibility{}, err
		}
		filename := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(filename)
		if err != nil {
			return Reproducibility{}, fmt.Errorf("inspecting an untracked study-source file: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filename)
			if err != nil {
				return Reproducibility{}, fmt.Errorf("reading an untracked symlink: %w", err)
			}
			if _, err := dirtyDigest.Write([]byte("symlink\x00")); err != nil {
				return Reproducibility{}, err
			}
			if err := writeField(dirtyDigest, []byte(target)); err != nil {
				return Reproducibility{}, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return Reproducibility{}, fmt.Errorf("untracked study-source entry is not a regular file")
		}
		if _, err := dirtyDigest.Write([]byte("file\x00")); err != nil {
			return Reproducibility{}, err
		}
		binaryLength := make([]byte, 8)
		binary.BigEndian.PutUint64(binaryLength, uint64(info.Size()))
		if _, err := dirtyDigest.Write(binaryLength); err != nil {
			return Reproducibility{}, err
		}
		file, err := os.Open(filename)
		if err != nil {
			return Reproducibility{}, fmt.Errorf("opening an untracked study-source file: %w", err)
		}
		_, copyErr := copyContext(parent, dirtyDigest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return Reproducibility{}, fmt.Errorf("hashing an untracked study-source file: %w", copyErr)
		}
		if closeErr != nil {
			return Reproducibility{}, fmt.Errorf("closing an untracked study-source file: %w", closeErr)
		}
	}
	retrievalSource, err := os.ReadFile(filepath.Join(root, "internal", "intelligence", "retrieval", "index.go"))
	if err != nil {
		return Reproducibility{}, fmt.Errorf("reading evaluated retrieval source: %w", err)
	}
	retrievalDigest := sha256.Sum256(retrievalSource)
	dirtyHex := hex.EncodeToString(dirtyDigest.Sum(nil))
	return Reproducibility{
		StudySourceCommit: strings.TrimSpace(commit),
		DirtyDiffSHA256: dirtyHex, ManifestSHA256: manifestSHA256,
		RetrievalSourceSHA256: hex.EncodeToString(retrievalDigest[:]),
	}, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64<<10)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			copied, writeErr := destination.Write(buffer[:count])
			written += int64(copied)
			if writeErr != nil {
				return written, writeErr
			}
			if copied != count {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func hashGitOutput(parent context.Context, timeout time.Duration, repository string, destination hash.Hash, arguments ...string) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := execCommand(ctx, repository, arguments...)
	command.Stdout = destination
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("git %s failed: %w", strings.Join(arguments, " "), err)
	}
	return nil
}

func gitTextBytes(parent context.Context, timeout time.Duration, repository string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	output, err := execCommand(ctx, repository, arguments...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git %s failed: %w", strings.Join(arguments, " "), err)
	}
	return output, nil
}

func execCommand(ctx context.Context, repository string, arguments ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", append([]string{"-C", repository}, arguments...)...)
}

func writeField(destination hash.Hash, value []byte) error {
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(value)))
	if _, err := destination.Write(length); err != nil {
		return err
	}
	_, err := destination.Write(value)
	return err
}
