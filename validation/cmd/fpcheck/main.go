package main

import (
	"bytes"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxOutput = 64 << 20

var ruleSeverity = map[string]string{
	"concurrency-01": "error", "concurrency-02": "error", "concurrency-03": "warning", "concurrency-04": "warning", "concurrency-05": "warning", "concurrency-06": "error", "concurrency-07": "error", "concurrency-08": "error", "concurrency-09": "warning", "concurrency-10": "info", "concurrency-12": "warning", "concurrency-14": "error", "concurrency-15": "warning", "concurrency-17": "warning", "concurrency-18": "warning", "concurrency-19": "error", "concurrency-20": "warning",
	"errors-01": "warning", "errors-02": "error", "errors-03": "info", "errors-04": "error", "errors-05": "warning", "errors-06": "info", "errors-07": "info", "errors-09": "error", "errors-10": "error", "errors-11": "error", "errors-12": "warning", "errors-13": "warning", "errors-14": "warning", "errors-15": "error", "errors-16": "error", "errors-17": "warning", "errors-19": "error",
}

type manifestRow struct {
	Project, Repository, ScanRoot, Patterns, Commit string
}

type finding struct {
	Project, Rule, Severity, File, Line, Posn, Message string
}

type cappedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := maxOutput - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return 0, io.ErrShortWrite
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.exceeded = true
		return remaining, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}

func main() {
	corpus := flag.String("corpus-root", "", "root containing named checkouts")
	manifest := flag.String("manifest", "", "explicit CSV manifest path")
	output := flag.String("output", "", "output findings directory")
	vettool := flag.String("vettool", os.Getenv("AGENTIC_GO_VET"), "existing agentic-go-vet binary")
	goBin := flag.String("go", "go", "Go binary used for build and scans")
	flag.Parse()
	if err := run(*corpus, *manifest, *output, *vettool, *goBin); err != nil {
		fmt.Fprintln(os.Stderr, "fpcheck:", err)
		os.Exit(1)
	}
}

func run(corpus, manifest, output, vettool, goBin string) error {
	if corpus == "" || manifest == "" || output == "" {
		return errors.New("-corpus-root, -manifest, and -output are required")
	}
	goPath, err := resolveBinary(goBin)
	if err != nil {
		return err
	}
	rows, err := readManifest(manifest)
	if err != nil {
		return err
	}
	if vettool == "" {
		tmp, err := os.MkdirTemp("", "agentic-go-vet-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		vettool = filepath.Join(tmp, "agentic-go-vet")
		cmd := exec.Command(goPath, "build", "-o", vettool, "./cmd/agentic-go-vet")
		cmd.Dir = repoRoot()
		cmd.Env = controlledEnv(goPath, os.Environ())
		_, stderr, err := runCommand(cmd)
		if err != nil {
			return fmt.Errorf("build vettool: %w: %s", err, stderr)
		}
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	findingsDir := filepath.Join(output, "findings")
	if err := os.RemoveAll(findingsDir); err != nil {
		return fmt.Errorf("reset findings directory: %w", err)
	}
	if err := os.Remove(filepath.Join(output, "unclassified.csv")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset unclassified findings: %w", err)
	}
	if err := os.MkdirAll(findingsDir, 0o755); err != nil {
		return err
	}
	byProject := map[string][]manifestRow{}
	for _, row := range rows {
		byProject[row.Project] = append(byProject[row.Project], row)
	}
	projects := make([]string, 0, len(byProject))
	for p := range byProject {
		projects = append(projects, p)
	}
	sort.Strings(projects)
	var all []finding
	goVersion := commandText(exec.Command(goPath, "version"))
	agenticHead := commandText(exec.Command("git", "-C", repoRoot(), "rev-parse", "HEAD"))
	toolVersion := commandText(exec.Command(vettool, "-V=full"))
	toolVersion = sanitizeText(toolVersion, "", vettool)
	for _, project := range projects {
		if filepath.Base(project) != project || project == "." || project == ".." {
			return fmt.Errorf("project name is not a safe output name: %q", project)
		}
		var raw bytes.Buffer
		seen := map[string]bool{}
		for _, row := range byProject[project] {
			checkout, err := checkoutPath(corpus, row.Repository)
			if err != nil {
				return err
			}
			if verifyErr := verifyCheckout(checkout, row.Commit); verifyErr != nil {
				return fmt.Errorf("%s: %w", project, verifyErr)
			}
			scanDir, err := containedPath(checkout, row.ScanRoot)
			if err != nil {
				return fmt.Errorf("%s: invalid scan root: %w", project, err)
			}
			scanDir, err = realContained(checkout, scanDir)
			if err != nil {
				return fmt.Errorf("%s: invalid scan root: %w", project, err)
			}
			tokens := strings.Fields(row.Patterns)
			for _, token := range tokens {
				if strings.HasPrefix(token, "-") {
					return fmt.Errorf("%s: pattern token begins with '-': %q", project, token)
				}
			}
			args := append([]string{"-json"}, tokens...)
			cmd := exec.Command(vettool, args...)
			cmd.Dir = scanDir
			cmd.Env = controlledEnv(goPath, os.Environ())
			stdout, stderr, err := runCommand(cmd)
			fmt.Fprintf(&raw, "# checkout HEAD: %s\n# scan root: %s\n# patterns: %s\n# selected Go: %s\n# agentic-go HEAD: %s\n# tool version: %s\n# agentic-go-vet: $AGENTIC_GO_VET\n# command: $AGENTIC_GO_VET %s\n# stderr:\n", row.Commit, row.ScanRoot, row.Patterns, strings.TrimSpace(goVersion), strings.TrimSpace(agenticHead), strings.TrimSpace(toolVersion), strings.Join(args, " "))
			raw.WriteString(strings.TrimRight(sanitizeText(string(stderr), checkout, vettool), "\r\n"))
			raw.WriteString("\n# stdout:\n")
			raw.WriteString(strings.TrimRight(sanitizeText(string(stdout), checkout, vettool), "\r\n"))
			raw.WriteByte('\n')
			if err != nil && !isDiagnosticExit(err) {
				if writeErr := atomicWrite(filepath.Join(findingsDir, project+"-findings.txt"), raw.Bytes()); writeErr != nil {
					return fmt.Errorf("%s (%s): %w; persist raw output: %v", project, row.Patterns, err, writeErr)
				}
				return fmt.Errorf("%s (%s): %w", project, row.Patterns, err)
			}
			fs, err := parseFindings(stdout, checkout)
			if err != nil {
				if writeErr := atomicWrite(filepath.Join(findingsDir, project+"-findings.txt"), raw.Bytes()); writeErr != nil {
					return fmt.Errorf("%s (%s): %w; persist raw output: %v", project, row.Patterns, err, writeErr)
				}
				return fmt.Errorf("%s (%s): %w", project, row.Patterns, err)
			}
			for _, f := range fs {
				key := f.Rule + "\x00" + f.Posn + "\x00" + f.Message
				if !seen[key] {
					seen[key] = true
					f.Project = project
					all = append(all, f)
				}
			}
		}
		if err := atomicWrite(filepath.Join(findingsDir, project+"-findings.txt"), raw.Bytes()); err != nil {
			return err
		}
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		return strings.Join([]string{a.Project, a.Rule, a.File, a.Line, a.Message}, "\x00") < strings.Join([]string{b.Project, b.Rule, b.File, b.Line, b.Message}, "\x00")
	})
	return writeCSV(filepath.Join(output, "unclassified.csv"), all)
}

func repoRoot() string { wd, _ := os.Getwd(); return wd }
func resolveBinary(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", name, err)
	}
	return filepath.Abs(p)
}

func controlledEnv(goPath string, base []string) []string {
	goDir := filepath.Dir(goPath)
	pathValue := os.Getenv("PATH")
	out := make([]string, 0, len(base)+4)
	for _, item := range base {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if key == "PATH" {
			pathValue = strings.TrimPrefix(item, "PATH=")
		}
		switch key {
		case "PATH", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB":
			continue
		}
		out = append(out, item)
	}
	out = append(out, "PATH="+goDir+string(os.PathListSeparator)+pathValue, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	return out
}

func readManifest(path string) ([]manifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = 5
	h, err := r.Read()
	if err != nil || strings.Join(h, ",") != "project,repository,scan_root,patterns,commit" {
		return nil, errors.New("manifest header must be project,repository,scan_root,patterns,commit")
	}
	var out []manifestRow
	for n := 2; ; n++ {
		rec, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("manifest row %d: %w", n, e)
		}
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		if len(rec) != 5 || rec[0] == "" || rec[1] == "" || rec[2] == "" || rec[3] == "" || !validSHA(rec[4]) || hasFlagPattern(rec[3]) {
			return nil, fmt.Errorf("invalid manifest row %d", n)
		}
		out = append(out, manifestRow{rec[0], rec[1], rec[2], rec[3], rec[4]})
	}
	if len(out) == 0 {
		return nil, errors.New("manifest is empty")
	}
	return out, nil
}

func hasFlagPattern(patterns string) bool {
	for _, token := range strings.Fields(patterns) {
		if strings.HasPrefix(token, "-") {
			return true
		}
	}
	return false
}

func validSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func checkoutPath(root, name string) (string, error) {
	p, err := containedPath(root, name)
	if err != nil {
		return "", errors.New("repository escapes corpus root")
	}
	return realContained(root, p)
}

func containedPath(root, name string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p, err := filepath.Abs(filepath.Join(base, name))
	if err != nil || (p != base && !strings.HasPrefix(p, base+string(os.PathSeparator))) {
		return "", errors.New("path escapes root")
	}
	return p, nil
}

func realContained(root, path string) (string, error) {
	rb, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rp, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if rp != rb && !strings.HasPrefix(rp, rb+string(os.PathSeparator)) {
		return "", errors.New("symlink-resolved path escapes root")
	}
	return rp, nil
}

func verifyCheckout(path, want string) error {
	cmd := exec.Command("git", "-C", path, "rev-parse", "HEAD")
	got, _, err := runCommand(cmd)
	if err != nil || strings.TrimSpace(string(got)) != want {
		return errors.New("HEAD does not match manifest commit")
	}
	cmd = exec.Command("git", "-C", path, "status", "--porcelain")
	out, _, err := runCommand(cmd)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return errors.New("checkout is dirty")
	}
	return nil
}

func runCommand(cmd *exec.Cmd) ([]byte, []byte, error) {
	var out, errOut cappedBuffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if out.exceeded || errOut.exceeded {
		return out.Bytes(), errOut.Bytes(), errors.New("command output exceeded 64 MiB")
	}
	return out.Bytes(), errOut.Bytes(), err
}

func isDiagnosticExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 3
}

func commandText(cmd *exec.Cmd) string {
	out, _, err := runCommand(cmd)
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}

func parseFindings(data []byte, checkout string) ([]finding, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var out []finding
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			if e, ok := x["error"]; ok && e != nil {
				return fmt.Errorf("analyzer JSON contains an error: %v", e)
			}
			if category, ok := x["category"].(string); ok {
				msg, mok := x["message"].(string)
				posn, pok := x["posn"].(string)
				if !mok || !pok || msg == "" {
					return errors.New("malformed diagnostic")
				}
				sev, ok := ruleSeverity[category]
				if !ok {
					return fmt.Errorf("unknown analyzer rule %q", category)
				}
				file, line, err := position(posn, checkout)
				if err != nil {
					return err
				}
				out = append(out, finding{Rule: category, Severity: sev, File: file, Line: line, Posn: posn, Message: sanitizeText(msg, checkout, "")})
			}
			for _, child := range x {
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range x {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

func sanitizeText(value, checkout, vettool string) string {
	if checkout != "" {
		value = strings.ReplaceAll(value, filepath.Clean(checkout)+string(os.PathSeparator), "")
		value = strings.ReplaceAll(value, filepath.ToSlash(filepath.Clean(checkout))+"/", "")
	}
	if vettool != "" {
		value = strings.ReplaceAll(value, vettool, "$AGENTIC_GO_VET")
	}
	return value
}

func position(posn, checkout string) (string, string, error) {
	last := strings.LastIndexByte(posn, ':')
	if last <= 0 {
		return "", "", errors.New("malformed diagnostic position")
	}
	prev := strings.LastIndexByte(posn[:last], ':')
	if prev <= 0 {
		return "", "", errors.New("malformed diagnostic position")
	}
	line := posn[prev+1 : last]
	if _, err := strconv.Atoi(line); err != nil {
		return "", "", errors.New("malformed diagnostic line")
	}
	if _, err := strconv.Atoi(posn[last+1:]); err != nil {
		return "", "", errors.New("malformed diagnostic column")
	}
	file := posn[:prev]
	realFile, err := realContained(checkout, file)
	if err != nil {
		return "", "", fmt.Errorf("diagnostic path outside checkout: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(realRoot, realFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("diagnostic path outside checkout")
	}
	return filepath.ToSlash(rel), line, nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".fpcheck-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeCSV(path string, fs []finding) error {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.Write([]string{"project", "rule", "severity", "file", "line", "message", "label", "notes"}); err != nil {
		return err
	}
	for _, f := range fs {
		if err := w.Write([]string{f.Project, f.Rule, f.Severity, f.File, f.Line, f.Message, "", ""}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return atomicWrite(path, b.Bytes())
}
