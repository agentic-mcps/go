package parser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const maxCoverageLine = 1 << 20

// CoverageBlock describes one source span from a Go coverage profile.
type CoverageBlock struct {
	File                string
	StartLine, StartCol int
	EndLine, EndCol     int
	Statements, Count   uint64
}

// CoverageGap describes an uncovered source span.
type CoverageGap struct {
	File                string
	StartLine, StartCol int
	EndLine, EndCol     int
	Statements          uint64
}

// CoverageFile contains coverage and gaps for one source file.
//
//nolint:govet // Keep the stable output field order.
type CoverageFile struct {
	File    string
	Percent float64
	Gaps    []CoverageGap
}

// CoverageReport contains the parsed coverage profile.
type CoverageReport struct {
	Files          []CoverageFile
	OverallPercent float64
}

// ParseCoverage parses a Go coverage profile from r.
func ParseCoverage(r io.Reader) (CoverageReport, error) {
	blocks, err := parseCoverageBlocks(r)
	if err != nil {
		return CoverageReport{}, err
	}
	byFile := make(map[string][]CoverageBlock)
	for _, b := range blocks {
		if b.Statements == 0 {
			continue
		}
		byFile[b.File] = append(byFile[b.File], b)
	}

	files := make([]CoverageFile, 0, len(byFile))
	var total, covered uint64
	for file, bs := range byFile {
		var statements, hit uint64
		gaps := make([]CoverageGap, 0)
		for _, b := range bs {
			var ok bool
			statements, ok = addUint(statements, b.Statements)
			if !ok {
				return CoverageReport{}, errors.New("coverage: statement total overflow")
			}
			if b.Count > 0 {
				hit, ok = addUint(hit, b.Statements)
				if !ok {
					return CoverageReport{}, errors.New("coverage: covered total overflow")
				}
			} else {
				gaps = append(gaps, CoverageGap{File: b.File, StartLine: b.StartLine, StartCol: b.StartCol, EndLine: b.EndLine, EndCol: b.EndCol, Statements: b.Statements})
			}
		}
		sort.Slice(gaps, func(i, j int) bool {
			if gaps[i].StartLine == gaps[j].StartLine {
				return gaps[i].StartCol < gaps[j].StartCol
			}
			return gaps[i].StartLine < gaps[j].StartLine
		})
		var ok bool
		total, ok = addUint(total, statements)
		if !ok {
			return CoverageReport{}, errors.New("coverage: overall statement total overflow")
		}
		covered, ok = addUint(covered, hit)
		if !ok {
			return CoverageReport{}, errors.New("coverage: overall covered total overflow")
		}
		files = append(files, CoverageFile{File: file, Percent: 100 * float64(hit) / float64(statements), Gaps: gaps})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	overallPercent := float64(0)
	if total > 0 {
		overallPercent = 100 * float64(covered) / float64(total)
	}
	return CoverageReport{Files: files, OverallPercent: overallPercent}, nil
}

// ParseCoverageBlocks parses and validates a Go coverage profile, returning
// every block, including blocks with zero execution count. The input mode is
// validated even though it does not alter block parsing.
func ParseCoverageBlocks(r io.Reader) ([]CoverageBlock, error) {
	return parseCoverageBlocks(r)
}

func parseCoverageBlocks(r io.Reader) ([]CoverageBlock, error) {
	if r == nil {
		return nil, errors.New("coverage: nil reader")
	}
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxCoverageLine)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return nil, fmt.Errorf("coverage: scan: %w", err)
		}
		return nil, errors.New("coverage: missing mode header")
	}
	header := s.Text()
	if !strings.HasPrefix(header, "mode: ") || header != strings.TrimSpace(header) {
		return nil, errors.New("coverage: invalid mode header")
	}
	mode := strings.TrimPrefix(header, "mode: ")
	if mode != "set" && mode != "count" && mode != "atomic" {
		return nil, fmt.Errorf("coverage: unsupported mode %q", mode)
	}
	blocks := make([]CoverageBlock, 0)
	for line := 2; s.Scan(); line++ {
		text := s.Text()
		fields := strings.Fields(text)
		if len(fields) != 3 || strings.Join(fields, " ") != text {
			return nil, fmt.Errorf("coverage: malformed line %d", line)
		}
		b, err := parseCoverageBlock(fields)
		if err != nil {
			return nil, fmt.Errorf("coverage: line %d: %w", line, err)
		}
		blocks = append(blocks, b)
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("coverage: scan: %w", err)
	}
	if len(blocks) == 0 {
		return nil, errors.New("coverage: profile has no blocks")
	}
	return blocks, nil
}

func parseCoverageBlock(fields []string) (CoverageBlock, error) {
	rangeText := fields[0]
	colon := strings.LastIndexByte(rangeText, ':')
	if colon <= 0 {
		return CoverageBlock{}, errors.New("invalid file/range")
	}
	file, coords := rangeText[:colon], rangeText[colon+1:]
	parts := strings.Split(coords, ",")
	if len(parts) != 2 {
		return CoverageBlock{}, errors.New("invalid range")
	}
	start, err := parseCoordinate(parts[0])
	if err != nil {
		return CoverageBlock{}, err
	}
	end, err := parseCoordinate(parts[1])
	if err != nil {
		return CoverageBlock{}, err
	}
	if end.line < start.line || (end.line == start.line && end.col < start.col) {
		return CoverageBlock{}, errors.New("range ends before it starts")
	}
	statements, err := parseUint(fields[1])
	if err != nil {
		return CoverageBlock{}, errors.New("invalid statements")
	}
	count, err := parseUint(fields[2])
	if err != nil {
		return CoverageBlock{}, errors.New("invalid count")
	}
	return CoverageBlock{File: file, StartLine: start.line, StartCol: start.col, EndLine: end.line, EndCol: end.col, Statements: statements, Count: count}, nil
}

type coordinate struct{ line, col int }

func parseCoordinate(s string) (coordinate, error) {
	p := strings.Split(s, ".")
	if len(p) != 2 {
		return coordinate{}, errors.New("invalid coordinate")
	}
	line, err := strconv.ParseUint(p[0], 10, 31)
	if err != nil || line == 0 {
		return coordinate{}, errors.New("invalid line")
	}
	col, err := strconv.ParseUint(p[1], 10, 31)
	if err != nil || col == 0 {
		return coordinate{}, errors.New("invalid column")
	}
	return coordinate{int(line), int(col)}, nil
}
func parseUint(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }
func addUint(a, b uint64) (uint64, bool) { c := a + b; return c, c >= a }
