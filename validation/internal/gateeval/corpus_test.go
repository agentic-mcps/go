package gateeval

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadCorpus(t *testing.T) {
	good := "project,url,pinned\ncobra,https://example.com/cobra,abc\nchi,/tmp/chi,def\n"
	tests := []struct { //nolint:govet // Table layout favors readability.
		name    string
		csv     string
		want    []Project
		wantErr string
	}{
		{
			name: "rows in file order",
			csv:  good,
			want: []Project{{Name: "cobra", URL: "https://example.com/cobra", Pinned: "abc"}, {Name: "chi", URL: "/tmp/chi", Pinned: "def"}},
		},
		{name: "wrong header", csv: "name,url,sha\na,b,c\n", wantErr: "header"},
		{name: "empty field", csv: "project,url,pinned\na,,c\n", wantErr: "empty field"},
		{name: "short row", csv: "project,url,pinned\na,b\n", wantErr: "fields"},
		{name: "duplicate project", csv: "project,url,pinned\na,b,c\na,d,e\n", wantErr: "duplicate"},
		{name: "empty file", csv: "", wantErr: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corpus.csv")
			if err := os.WriteFile(path, []byte(tt.csv), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadCorpus(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("projects = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadCorpusShippedFile(t *testing.T) {
	projects, err := LoadCorpus(filepath.Join("..", "..", "gate", "corpus.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 5 {
		t.Fatalf("projects = %d, want the five protocol repositories", len(projects))
	}
	for _, project := range projects {
		if len(project.Pinned) != 40 {
			t.Errorf("%s pinned = %q, want a full hash", project.Name, project.Pinned)
		}
	}
}
