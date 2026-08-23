package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInitDisabledDoesNoWork(t *testing.T) {
	t.Setenv(traceEnv, "false")
	tracer, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	if err := tracer.Record(Event{Tool: "secret-tool", Args: func() {}}); err != nil {
		t.Fatalf("disabled Record() inspected unmarshalable arguments: %v", err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordShapeAndPrivacy(t *testing.T) {
	base := t.TempDir()
	tracer, err := NewWithBaseDir(base)
	if err != nil {
		t.Fatal(err)
	}
	secret := "/private/workspace/secret.go"
	if err := tracer.Record(Event{
		Tool:               "go_race_report",
		Args:               map[string]string{"path": secret},
		Duration:           142 * time.Millisecond,
		PackagesLoad:       38 * time.Millisecond,
		Analysis:           96 * time.Millisecond,
		FindingsBySeverity: map[string]int{"error": 1},
		AnalyzerDurations:  map[string]time.Duration{"concurrency": 6 * time.Millisecond},
		ResultSummary:      "3 findings",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}

	line := readTrace(t, base)
	if strings.Contains(line, secret) {
		t.Fatalf("trace leaked raw arguments: %s", line)
	}
	var got Record
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.ArgsHash, "sha256:") || got.Tool != "go_race_report" || got.DurationMS != 142 {
		t.Fatalf("unexpected record: %+v", got)
	}
	if got.PackagesLoadMS != 38 || got.AnalysisMS != 96 || got.AnalyzerDurationsMS["concurrency"] != 6 {
		t.Fatalf("missing timing data: %+v", got)
	}
	if got.Error || got.ErrorKind != ErrorNone || got.ResultSummary != "3 findings" {
		t.Fatalf("unexpected result fields: %+v", got)
	}
}

func TestRecordRejectsRawErrorShapedState(t *testing.T) {
	tracer, err := NewWithBaseDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tracer.Close() }()

	err = tracer.Record(Event{Tool: "go_test_structured", ErrorKind: ErrorSubprocess, ResultSummary: "raw failure"})
	if err == nil {
		t.Fatal("Record() accepted both an error and result summary")
	}
}

func TestConcurrentRecords(t *testing.T) {
	base := t.TempDir()
	tracer, err := NewWithBaseDir(base)
	if err != nil {
		t.Fatal(err)
	}
	const count = 100
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			if err := tracer.Record(Event{Tool: "tool", Args: i, Duration: time.Millisecond, ResultSummary: "ok"}); err != nil {
				t.Errorf("Record() error = %v", err)
			}
		}(i)
	}
	wait.Wait()
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(readTrace(t, base)), "\n")
	if len(lines) != count {
		t.Fatalf("got %d records, want %d", len(lines), count)
	}
}

func TestCloseStopsRecords(t *testing.T) {
	base := t.TempDir()
	tracer, err := NewWithBaseDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Record(Event{Tool: "after-close", ResultSummary: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
	if line := readTrace(t, base); line != "" {
		t.Fatalf("record written after close: %s", line)
	}
}

func TestNewWithBaseDirReportsCreationFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithBaseDir(file); err == nil {
		t.Fatal("NewWithBaseDir() hid filesystem failure")
	}
}

func readTrace(t *testing.T, base string) string {
	t.Helper()
	var path string
	if err := filepath.WalkDir(base, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == traceFile {
			path = current
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("trace file not found")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(contents))
}
