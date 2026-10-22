package gate

import (
	"reflect"
	"testing"
)

func TestMapListErrors(t *testing.T) {
	root := "/work/repo"
	wrap := func(stderr string) string {
		return "loading active package scope: listing packages in /work/repo: go list exited 1: " + stderr
	}
	cases := []struct {
		name string
		text string
		want []Item
	}{
		{
			name: "positioned import error",
			text: wrap("lib/lib.go:3:8: package fmtt is not in std (/usr/local/go/src/fmtt)"),
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild, File: "lib/lib.go", Line: 3,
				Message: "package does not load: package fmtt is not in std", Fix: mapListFix,
				Detail: "lib/lib.go:3:8: package fmtt is not in std (/usr/local/go/src/fmtt)",
			}},
		},
		{
			name: "absolute path without a column",
			text: wrap("/work/repo/app/app.go:3: found packages lib (lib.go) and other (other.go) in /work/repo/lib"),
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild, File: "app/app.go", Line: 3,
				Message: "package does not load: found packages lib (lib.go) and other (other.go) in /work/repo/lib", Fix: mapListFix,
				Detail: "/work/repo/app/app.go:3: found packages lib (lib.go) and other (other.go) in /work/repo/lib",
			}},
		},
		{
			name: "import cycle without a position",
			text: wrap("package m/app\n\timports m/lib from app.go\n\timports m/app from lib.go: import cycle not allowed"),
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild,
				Message: "package does not load: imports m/app from lib.go: import cycle not allowed", Fix: mapListFix,
				Detail: "package m/app\n\timports m/lib from app.go\n\timports m/app from lib.go: import cycle not allowed",
			}},
		},
		{
			name: "near miss: network failure never blocks",
			text: wrap("lib/lib.go:3:8: github.com/x/y@v1.0.0: Get \"https://proxy.golang.org/x\": dial tcp: lookup proxy.golang.org: no such host"),
		},
		{name: "near miss: unrecognized go command error", text: wrap("go: updates to go.mod needed; to update it:\n\tgo mod tidy")},
		{name: "near miss: not a go list failure", text: "resolving module metadata directory: permission denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapListErrors(root, tc.text)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("items = %#v, want none", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("items =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}
