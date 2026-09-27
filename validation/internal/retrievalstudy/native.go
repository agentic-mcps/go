package retrievalstudy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	nativeOutputLimitDefault = int64(64 << 20)
	nativeLineLimitDefault   = 100000
)

var nativeGlobs = []string{
	"*.go", "*.md", "*.rst", "*.txt", "*.yaml", "*.yml", "*.json",
	"*.toml", "*.proto", "*.mod", "*.sum", "*.work", "Makefile", "GNUmakefile",
	"Dockerfile", "Containerfile", ".gitignore", ".editorconfig", ".gitattributes",
}

type nativeLimits struct {
	timeout     time.Duration
	outputBytes int64
	lineCount   int
}

type nativeMeasurement struct {
	ranking        Ranking
	totalLatency   Latency
	commandLatency Latency
	rankingLatency Latency
}

type rgLine struct {
	path string
	line int
	text string
}

// ProbeRG returns the first version line without recording the executable path.
func ProbeRG(parent context.Context, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "rg", "--version")
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("probing ripgrep: %w", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	return line, nil
}

func RunNativeRG(parent context.Context, workspace string, query Query, source archivedSource, gold []GoldSpan, repetitions int, limits nativeLimits) (nativeMeasurement, error) {
	tokens := uniqueSortedTokens(query.Text)
	commandSamples := make([]float64, 0, repetitions)
	rankingSamples := make([]float64, 0, repetitions)
	totalSamples := make([]float64, 0, repetitions)
	var finalCandidates []Candidate
	var finalCount int
	complete := true
	var incompleteReason string
	for sample := 0; sample < repetitions; sample++ {
		if err := parent.Err(); err != nil {
			return nativeMeasurement{}, err
		}
		lines, gatherDuration, runComplete, runReason, err := gatherRG(parent, workspace, tokens, source, limits)
		if err != nil {
			return nativeMeasurement{}, err
		}
		commandMS := float64(gatherDuration) / float64(time.Millisecond)
		commandSamples = append(commandSamples, commandMS)
		startRank := time.Now()
		candidates := rankRGLines(lines, tokens)
		rankingDuration := time.Since(startRank)
		rankingMS := float64(rankingDuration) / float64(time.Millisecond)
		rankingSamples = append(rankingSamples, rankingMS)
		totalSamples = append(totalSamples, commandMS+rankingMS)
		finalCandidates = candidates.top
		finalCount = candidates.total
		if !runComplete {
			complete = false
			incompleteReason = runReason
		}
	}
	ranking := scoreRanking(finalCandidates, finalCount, gold, complete, incompleteReason, "matching_source_line", tokens)
	ranking.CandidateCountComplete = complete && source.coverage.SourceArchiveComplete
	if !source.coverage.SourceArchiveComplete {
		ranking.Complete = false
		ranking.MetricsUsable = false
		ranking.Status = "partial"
		if ranking.IncompleteReason == "" {
			ranking.IncompleteReason = source.coverage.SourceArchiveIncompleteReason
		}
	}
	return nativeMeasurement{
		ranking: ranking,
		totalLatency: latency(totalSamples),
		commandLatency: latency(commandSamples),
		rankingLatency: latency(rankingSamples),
	}, nil
}

func gatherRG(parent context.Context, workspace string, tokens []string, source archivedSource, limits nativeLimits) ([]rgLine, time.Duration, bool, string, error) {
	arguments := []string{
		"--no-ignore", "--hidden", "--glob-case-insensitive", "--no-heading", "--with-filename", "--line-number",
		"--color", "never", "--fixed-strings", "--ignore-case", "--text", "--null",
	}
	for _, glob := range nativeGlobs {
		arguments = append(arguments, "--glob="+glob)
	}
	for _, token := range tokens {
		arguments = append(arguments, "-e", token)
	}
	arguments = append(arguments, "--", ".")
	ctx, cancel := context.WithTimeout(parent, limits.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "rg", arguments...)
	command.Dir = workspace
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, 0, false, "", fmt.Errorf("opening ripgrep output: %w", err)
	}
	start := time.Now()
	if err := command.Start(); err != nil {
		return nil, 0, false, "", fmt.Errorf("starting ripgrep: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	lines := make([]rgLine, 0)
	var bytesRead int64
	complete := true
	reason := ""
	for scanner.Scan() {
		raw := scanner.Bytes()
		bytesRead += int64(len(raw) + 1)
		if bytesRead > limits.outputBytes {
			complete = false
			reason = "ripgrep output exceeded the configured byte limit"
			_ = command.Process.Kill()
			break
		}
		if len(lines) >= limits.lineCount {
			complete = false
			reason = "ripgrep output exceeded the configured matching-line limit"
			_ = command.Process.Kill()
			break
		}
		parsed, ok := parseRGLine(raw)
		if !ok {
			complete = false
			reason = "ripgrep emitted a line that did not match the recorded output format"
			_ = command.Process.Kill()
			break
		}
		meta, supported := source.meta[parsed.path]
		if !supported || parsed.line < 1 || parsed.line > meta.lines {
			complete = false
			reason = "ripgrep returned a path or line outside the archived supported-source manifest"
			_ = command.Process.Kill()
			break
		}
		lines = append(lines, parsed)
	}
	if scanner.Err() != nil && complete {
		complete = false
		reason = "ripgrep output line exceeded the 4 MiB line limit"
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	duration := time.Since(start)
	if parent.Err() != nil {
		return nil, duration, false, "", parent.Err()
	}
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		complete = false
		reason = "ripgrep exceeded the per-query timeout"
	} else if waitErr != nil && complete {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			// ripgrep uses exit code 1 for a successful search with no matches.
		} else {
			complete = false
			reason = "ripgrep exited unsuccessfully"
		}
	}
	return lines, duration, complete, reason, nil
}

func parseRGLine(raw []byte) (rgLine, bool) {
	separator := bytes.IndexByte(raw, 0)
	if separator <= 0 {
		return rgLine{}, false
	}
	pathValue := strings.TrimPrefix(string(raw[:separator]), "./")
	if path.Clean(pathValue) != pathValue {
		return rgLine{}, false
	}
	rest := raw[separator+1:]
	colon := bytes.IndexByte(rest, ':')
	if colon <= 0 {
		return rgLine{}, false
	}
	line, err := strconv.Atoi(string(rest[:colon]))
	if err != nil || line < 1 {
		return rgLine{}, false
	}
	return rgLine{path: pathValue, line: line, text: string(rest[colon+1:])}, true
}

type rankedLines struct {
	top   []Candidate
	total int
}

func rankRGLines(lines []rgLine, queryTokens []string) rankedLines {
	query := make(map[string]struct{}, len(queryTokens))
	for _, token := range queryTokens {
		query[token] = struct{}{}
	}
	candidates := make([]Candidate, 0, len(lines))
	for _, line := range lines {
		frequencies := make(map[string]int)
		for _, token := range Tokenize(line.text) {
			if _, included := query[token]; included {
				frequencies[token]++
			}
		}
		occurrences := 0
		for _, count := range frequencies {
			occurrences += count
		}
		candidates = append(candidates, Candidate{
			Path: line.path, Line: line.line,
			MatchedUniqueTerms: len(frequencies), MatchedOccurrences: occurrences,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.MatchedUniqueTerms != right.MatchedUniqueTerms {
			return left.MatchedUniqueTerms > right.MatchedUniqueTerms
		}
		if left.MatchedOccurrences != right.MatchedOccurrences {
			return left.MatchedOccurrences > right.MatchedOccurrences
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Line < right.Line
	})
	count := len(candidates)
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}
	return rankedLines{top: candidates, total: count}
}

func uniqueSortedTokens(text string) []string {
	seen := make(map[string]struct{})
	for _, token := range Tokenize(text) {
		seen[token] = struct{}{}
	}
	tokens := make([]string, 0, len(seen))
	for token := range seen {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}
