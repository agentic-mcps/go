package verification

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompilerErrorExcerpt(t *testing.T) {
	many := make([]string, 0, 30)
	for line := 1; line <= 30; line++ {
		many = append(many, fmt.Sprintf("a/a.go:%d:2: undefined: name%d", line, line))
	}
	long := make([]string, 0, 3)
	for line := 1; line <= 3; line++ {
		long = append(long, fmt.Sprintf("a/a.go:%d:2: %s", line, strings.Repeat("x", 1800)))
	}
	tests := []struct {
		name      string
		output    string
		want      string
		wantLines int
	}{
		{
			name:   "keeps positioned compiler lines and drops headers",
			output: "# example.test/a [example.test/a.test]\na/a_test.go:5:32: undefined: missing\n\thave ()\n",
			want:   "a/a_test.go:5:32: undefined: missing", wantLines: 1,
		},
		{
			name:   "keeps vet diagnostics",
			output: "# example.test/d\n# [example.test/d]\nd/d.go:5:39: fmt.Sprintf format %d has arg \"s\" of wrong type string\n",
			want:   "d/d.go:5:39: fmt.Sprintf format %d has arg \"s\" of wrong type string", wantLines: 1,
		},
		{
			name:   "unpositioned output is kept when nothing has a position",
			output: "# example.test/a\nsetup failed: no Go files\n",
			want:   "setup failed: no Go files", wantLines: 1,
		},
		{
			name:   "line positions without a column are not compiler positions",
			output: "a/a.go:5: not a compiler error\na/a.go:6:1: real error\n",
			want:   "a/a.go:6:1: real error", wantLines: 1,
		},
		{name: "bounds line count", output: strings.Join(many, "\n"), wantLines: maxCompilerErrorLines},
		{name: "bounds bytes on a line boundary", output: strings.Join(long, "\n"), wantLines: 2},
		{name: "empty output", output: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compilerErrorExcerpt(test.output)
			if test.want != "" && got != test.want {
				t.Fatalf("excerpt = %q, want %q", got, test.want)
			}
			if len(got) > maxCompilerErrorBytes {
				t.Fatalf("excerpt has %d bytes, want at most %d", len(got), maxCompilerErrorBytes)
			}
			lines := 0
			if got != "" {
				lines = strings.Count(got, "\n") + 1
			}
			if lines != test.wantLines {
				t.Fatalf("excerpt has %d lines, want %d:\n%s", lines, test.wantLines, got)
			}
		})
	}
}

func TestBuildImportPath(t *testing.T) {
	tests := map[string]string{
		"example.test/a [example.test/a.test]":      "example.test/a",
		"example.test/a":                            "example.test/a",
		"example.test/a_test [example.test/a.test]": "example.test/a_test",
	}
	for input, want := range tests {
		if got := buildImportPath(input); got != want {
			t.Fatalf("buildImportPath(%q) = %q, want %q", input, got, want)
		}
	}
}
