// Package trace records optional, privacy-preserving tool-call summaries.
package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	traceEnv            = "AGENTIC_GO_TRACE"
	traceFile           = "trace.jsonl"
	maxToolBytes        = 128
	maxSummaryBytes     = 256
	maxMapEntries       = 32
	maxMapKeyBytes      = 64
	defaultRelativeBase = "agentic-go/runs"
)

// ErrorKind is a bounded failure category. It never contains a raw error.
type ErrorKind string

const (
	// ErrorNone indicates that an operation completed without an error.
	ErrorNone ErrorKind = ""
	// ErrorInvalidInput indicates invalid caller input.
	ErrorInvalidInput ErrorKind = "invalid_input"
	// ErrorCancelled indicates caller cancellation.
	ErrorCancelled ErrorKind = "cancelled"
	// ErrorDeadline indicates an operation deadline.
	ErrorDeadline ErrorKind = "deadline"
	// ErrorSubprocess indicates a subprocess failure.
	ErrorSubprocess ErrorKind = "subprocess"
	// ErrorAnalysis indicates an analyzer failure.
	ErrorAnalysis ErrorKind = "analysis"
	// ErrorInternal indicates an unexpected internal failure.
	ErrorInternal ErrorKind = "internal"
)

// Event is the caller-facing trace interface. Args are hashed and never stored.
//
//nolint:govet // event field order mirrors the trace contract.
type Event struct {
	Tool               string
	Args               any
	Duration           time.Duration
	PackagesLoad       time.Duration
	Analysis           time.Duration
	FindingsBySeverity map[string]int
	AnalyzerDurations  map[string]time.Duration
	ResultSummary      string
	ErrorKind          ErrorKind
}

// Record is the deliberately bounded on-disk representation.
//
//nolint:govet // record field order mirrors the trace JSON contract.
type Record struct {
	Timestamp           time.Time        `json:"ts"`
	Tool                string           `json:"tool"`
	ArgsHash            string           `json:"args_hash"`
	DurationMS          int64            `json:"duration_ms"`
	PackagesLoadMS      int64            `json:"packages_load_ms"`
	AnalysisMS          int64            `json:"analysis_ms"`
	FindingsBySeverity  map[string]int   `json:"findings_by_severity"`
	AnalyzerDurationsMS map[string]int64 `json:"analyzer_durations_ms"`
	ResultSummary       string           `json:"result_summary"`
	Error               bool             `json:"error"`
	ErrorKind           ErrorKind        `json:"error_kind"`
}

// Tracer is safe for concurrent use. A zero Tracer is disabled.
//
//nolint:govet // mutex/encoder/file order reflects lifecycle ownership.
type Tracer struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	closed  bool
}

// Init reads AGENTIC_GO_TRACE once and uses the operating-system user cache.
func Init() (*Tracer, error) {
	if os.Getenv(traceEnv) != "true" {
		return &Tracer{}, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locating user cache: %w", err)
	}
	return newTracer(filepath.Join(base, defaultRelativeBase))
}

// NewWithBaseDir creates an enabled tracer beneath an explicit base directory.
// It exists so tests can exercise the same filesystem implementation locally.
func NewWithBaseDir(baseDir string) (*Tracer, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("trace base directory is empty")
	}
	return newTracer(baseDir)
}

func newTracer(baseDir string) (*Tracer, error) {
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating trace base directory: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(os.Getpid()) + "-"
	dir, err := os.MkdirTemp(baseDir, stamp)
	if err != nil {
		return nil, fmt.Errorf("creating trace run directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, traceFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating trace file: %w", err)
	}
	return &Tracer{file: file, encoder: json.NewEncoder(file)}, nil
}

// Record appends one bounded JSONL event. It is a no-op when disabled or closed.
func (t *Tracer) Record(event Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil || t.closed {
		return nil
	}
	if !validErrorKind(event.ErrorKind) {
		return fmt.Errorf("invalid trace error kind %q", event.ErrorKind)
	}
	if event.ErrorKind != ErrorNone && event.ResultSummary != "" {
		return fmt.Errorf("failed trace event must not include a result summary")
	}

	args, err := json.Marshal(event.Args)
	if err != nil {
		return fmt.Errorf("hashing trace arguments: %w", err)
	}
	hash := sha256.Sum256(args)
	record := Record{
		Timestamp:           time.Now().UTC(),
		Tool:                bound(event.Tool, maxToolBytes),
		ArgsHash:            "sha256:" + hex.EncodeToString(hash[:]),
		DurationMS:          nonNegativeMillis(event.Duration),
		PackagesLoadMS:      nonNegativeMillis(event.PackagesLoad),
		AnalysisMS:          nonNegativeMillis(event.Analysis),
		FindingsBySeverity:  boundedInts(event.FindingsBySeverity),
		AnalyzerDurationsMS: boundedDurations(event.AnalyzerDurations),
		ResultSummary:       bound(event.ResultSummary, maxSummaryBytes),
		Error:               event.ErrorKind != ErrorNone,
		ErrorKind:           event.ErrorKind,
	}
	if err := t.encoder.Encode(record); err != nil {
		return fmt.Errorf("writing trace record: %w", err)
	}
	return nil
}

// Close flushes and closes the trace file. It is safe to call more than once.
func (t *Tracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.file == nil {
		return nil
	}
	if err := t.file.Sync(); err != nil {
		_ = t.file.Close()
		return fmt.Errorf("syncing trace file: %w", err)
	}
	if err := t.file.Close(); err != nil {
		return fmt.Errorf("closing trace file: %w", err)
	}
	return nil
}

func validErrorKind(kind ErrorKind) bool {
	switch kind {
	case ErrorNone, ErrorInvalidInput, ErrorCancelled, ErrorDeadline, ErrorSubprocess, ErrorAnalysis, ErrorInternal:
		return true
	default:
		return false
	}
}

func nonNegativeMillis(duration time.Duration) int64 {
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

func boundedInts(values map[string]int) map[string]int {
	result := make(map[string]int, min(len(values), maxMapEntries))
	for _, key := range sortedKeys(values) {
		if len(result) == maxMapEntries {
			break
		}
		value := values[key]
		if value < 0 {
			value = 0
		}
		result[bound(key, maxMapKeyBytes)] = value
	}
	return result
}

func boundedDurations(values map[string]time.Duration) map[string]int64 {
	result := make(map[string]int64, min(len(values), maxMapEntries))
	for _, key := range sortedKeys(values) {
		if len(result) == maxMapEntries {
			break
		}
		value := values[key]
		result[bound(key, maxMapKeyBytes)] = nonNegativeMillis(value)
	}
	return result
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func bound(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
