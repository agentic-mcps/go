package gate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const wantSyntaxFix = "Fix the syntax error; nothing else can be checked until the file parses."

func writeWorkspaceFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestCheckSyntax(t *testing.T) {
	t.Parallel()
	cases := []struct {
		files map[string]string
		want  []Item
		name  string
		paths []string
	}{
		{
			name:  "valid file has no items",
			files: map[string]string{"ok.go": "package p\n\nfunc F() {}\n"},
			paths: []string{"ok.go"},
			want:  []Item{},
		},
		{
			name:  "brace inside a string literal is valid",
			files: map[string]string{"ok.go": "package p\n\nvar s = \"{\"\n"},
			paths: []string{"ok.go"},
			want:  []Item{},
		},
		{
			name:  "unterminated function reports the first error, as gofmt does",
			files: map[string]string{"bad.go": "package p\n\nfunc F() {\n\tx := 1\n"},
			paths: []string{"bad.go"},
			want: []Item{{
				Severity: SeverityBlock,
				Code:     CodeSyntax,
				File:     "bad.go",
				Line:     4,
				Message:  "syntax error: expected ';', found 'EOF'",
				Fix:      wantSyntaxFix,
			}},
		},
		{
			name: "only the first error of a file is reported",
			files: map[string]string{
				"bad.go": "package p\n\nvar a = )\n\nvar b = )\n",
			},
			paths: []string{"bad.go"},
			want: []Item{{
				Severity: SeverityBlock,
				Code:     CodeSyntax,
				File:     "bad.go",
				Line:     3,
				Message:  "syntax error: expected operand, found ')'",
				Fix:      wantSyntaxFix,
			}},
		},
		{
			name: "two broken files give two items in input order",
			files: map[string]string{
				"a.go": "package p\n\nfunc A() {\n",
				"b.go": "package p\n\nvar b = )\n",
			},
			paths: []string{"b.go", "a.go"},
			want: []Item{
				{
					Severity: SeverityBlock,
					Code:     CodeSyntax,
					File:     "b.go",
					Line:     3,
					Message:  "syntax error: expected operand, found ')'",
					Fix:      wantSyntaxFix,
				},
				{
					Severity: SeverityBlock,
					Code:     CodeSyntax,
					File:     "a.go",
					Line:     3,
					Message:  "syntax error: expected ';', found 'EOF'",
					Fix:      wantSyntaxFix,
				},
			},
		},
		{
			name: "non-go and deleted files are skipped",
			files: map[string]string{
				"notes.txt": "{{{ not go",
				"bad.go.md": "{{{ not go",
				"good.go":   "package p\n",
			},
			paths: []string{"notes.txt", "bad.go.md", "deleted.go", "good.go"},
			want:  []Item{},
		},
		{
			name:  "nested slash path is read and reported with that path",
			files: map[string]string{"internal/x/bad.go": "package x\n\nfunc F() {\n"},
			paths: []string{"internal/x/bad.go"},
			want: []Item{{
				Severity: SeverityBlock,
				Code:     CodeSyntax,
				File:     "internal/x/bad.go",
				Line:     3,
				Message:  "syntax error: expected ';', found 'EOF'",
				Fix:      wantSyntaxFix,
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeWorkspaceFiles(t, root, tc.files)
			got := CheckSyntax(root, tc.paths)
			if got == nil {
				t.Fatal("CheckSyntax() returned nil, want non-nil slice")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CheckSyntax() =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestCheckSyntaxSkipsPathsOutsideWorkspace(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "ws")
	writeWorkspaceFiles(t, root, map[string]string{"ok.go": "package p\n"})
	writeWorkspaceFiles(t, parent, map[string]string{"outside.go": "package p\n\nfunc F() {\n"})

	got := CheckSyntax(root, []string{"../outside.go", "/etc/passwd.go", ""})
	if got == nil || len(got) != 0 {
		t.Fatalf("CheckSyntax() = %+v, want empty non-nil slice", got)
	}
}
