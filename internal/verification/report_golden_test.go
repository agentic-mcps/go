package verification_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

func TestGoldenReportsMatchPortableContract(t *testing.T) {
	tests := []struct {
		name     string
		status   verification.ResultStatus
		exitCode int
	}{
		{name: "report-pass.json", status: verification.ResultPass, exitCode: 0},
		{name: "report-findings.json", status: verification.ResultFindings, exitCode: 1},
		{name: "report-incomplete.json", status: verification.ResultIncomplete, exitCode: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join("testdata", test.name))
			if err != nil {
				t.Fatal(err)
			}
			var report verification.Report
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(&report)
			if err != nil {
				t.Fatalf("decode golden report: %v", err)
			}
			if report.SchemaVersion != verification.SchemaVersion || report.Result.Status != test.status {
				t.Fatalf("schema/status = %q/%q, want %q/%q", report.SchemaVersion, report.Result.Status, verification.SchemaVersion, test.status)
			}
			if report.Result.ExitCode != test.exitCode {
				t.Fatalf("exit code = %d, want %d", report.Result.ExitCode, test.exitCode)
			}
			if report.Change.Files == nil || report.Change.Declarations == nil || report.Impact.Packages == nil || report.Plan == nil || report.Evidence == nil || report.Findings == nil || report.Risks == nil || report.Uncertainties == nil {
				t.Fatal("golden report contains a nil collection")
			}
			finalized := report
			err = finalized.Finalize(verification.Policy{})
			if err != nil {
				t.Fatalf("finalize golden report: %v", err)
			}
			if !reflect.DeepEqual(finalized, report) {
				t.Fatalf("golden report is not the deterministic finalized report: id = %q, want %q", finalized.ID, report.ID)
			}
			canonical, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			canonicalAgain, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, canonicalAgain) {
				t.Fatal("canonical Go JSON encoding is not deterministic")
			}
		})
	}
}
