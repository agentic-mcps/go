package eval

import "testing"

func TestOracleDiscriminates(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   bool
	}{
		{name: "behavior fails", result: Result{Status: "fail", Commands: []CommandResult{{Status: "fail"}}}, want: true},
		{name: "scope only failure", result: Result{Status: "fail", Commands: []CommandResult{{Status: "pass"}}}},
		{name: "incomplete", result: Result{Status: "incomplete", Commands: []CommandResult{{Status: "incomplete"}}}},
		{name: "no commands", result: Result{Status: "fail"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := oracleDiscriminates(test.result); got != test.want {
				t.Fatalf("oracleDiscriminates() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestScopeProbeDiscriminates(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   bool
	}{
		{
			name: "scope alone fails",
			result: Result{
				Status:          "fail",
				UnexpectedPaths: []string{scopeProbePath},
				Commands:        []CommandResult{{Status: "pass"}},
			},
			want: true,
		},
		{
			name: "behavior also fails",
			result: Result{
				Status:          "fail",
				UnexpectedPaths: []string{scopeProbePath},
				Commands:        []CommandResult{{Status: "fail"}},
			},
		},
		{
			name: "extra unexpected path",
			result: Result{
				Status:          "fail",
				UnexpectedPaths: []string{scopeProbePath, "extra.go"},
				Commands:        []CommandResult{{Status: "pass"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scopeProbeDiscriminates(test.result); got != test.want {
				t.Fatalf("scopeProbeDiscriminates() = %v, want %v", got, test.want)
			}
		})
	}
}
