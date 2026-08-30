package tools

import (
	"testing"

	"github.com/agentic-mcps/go/internal/finding"
)

func TestNormalizeAuditInput(t *testing.T) {
	tests := []struct {
		name string
		in   auditOptions
		want auditOptions
		err  bool
	}{
		{name: "defaults", in: auditOptions{Package: "."}, want: auditOptions{Package: ".", MinSeverity: finding.SeverityInfo, MaxFindings: 200}},
		{name: "clamps", in: auditOptions{Package: ".", MinSeverity: finding.SeverityWarning, MaxFindings: 1001}, want: auditOptions{Package: ".", MinSeverity: finding.SeverityWarning, MaxFindings: 1000}},
		{name: "negative max", in: auditOptions{Package: ".", MaxFindings: -1}, err: true},
		{name: "unknown severity", in: auditOptions{Package: ".", MinSeverity: "critical"}, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			err := normalizeAuditInput(&got)
			if (err != nil) != tt.err {
				t.Fatalf("error = %v, want error %v", err, tt.err)
			}
			if !tt.err && got != tt.want {
				t.Fatalf("normalized = %+v, want %+v", got, tt.want)
			}
		})
	}
}
