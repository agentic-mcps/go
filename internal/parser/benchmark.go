package parser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxBenchmarkLine = 1 << 20

// BenchmarkSample is one parsed benchmark measurement.
type BenchmarkSample struct {
	NsOp float64
}

// BenchmarkResult groups measurements for one benchmark name.
type BenchmarkResult struct {
	Name    string
	Samples []BenchmarkSample
	Median  float64
}

// BenchmarkReport contains all parsed benchmark results.
type BenchmarkReport struct {
	Benchmarks []BenchmarkResult
}

var benchmarkLineRE = regexp.MustCompile(`^(Benchmark\S+)-[0-9]+\s+[0-9]+\s+(\S+)\s+ns/op(?:\s|$)`)

// ParseBenchmarks parses completed Go benchmark output from r.
func ParseBenchmarks(r io.Reader) (BenchmarkReport, error) {
	if r == nil {
		return BenchmarkReport{}, errors.New("benchmark: nil reader")
	}

	byName := make(map[string][]BenchmarkSample)
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxBenchmarkLine)
	for s.Scan() {
		line := s.Text()
		match := benchmarkLineRE.FindStringSubmatch(line)
		if match == nil {
			if strings.HasPrefix(line, "Benchmark") {
				return BenchmarkReport{}, errors.New("benchmark: malformed benchmark line")
			}
			continue
		}
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return BenchmarkReport{}, fmt.Errorf("benchmark: invalid ns/op value %q", match[2])
		}
		byName[match[1]] = append(byName[match[1]], BenchmarkSample{NsOp: value})
	}
	if err := s.Err(); err != nil {
		return BenchmarkReport{}, fmt.Errorf("benchmark: scan: %w", err)
	}
	if len(byName) == 0 {
		return BenchmarkReport{}, errors.New("benchmark: no valid benchmark lines")
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]BenchmarkResult, 0, len(names))
	for _, name := range names {
		samples := byName[name]
		sort.Slice(samples, func(i, j int) bool { return samples[i].NsOp < samples[j].NsOp })
		median := samples[len(samples)/2].NsOp
		if len(samples)%2 == 0 {
			median = samples[len(samples)/2-1].NsOp/2 + median/2
		}
		results = append(results, BenchmarkResult{Name: name, Samples: samples, Median: median})
	}
	return BenchmarkReport{Benchmarks: results}, nil
}
