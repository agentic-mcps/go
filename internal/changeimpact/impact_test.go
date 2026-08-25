package changeimpact_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/changeimpact"
)

func TestImpactIncludesTransitiveReverseImporters(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":       "module example.test/graph\n\ngo 1.25.0\n",
		"leaf/leaf.go": "package leaf\n\nfunc Value() int { return 1 }\n",
		"mid/mid.go": `package mid

import "example.test/graph/leaf"

func Value() int { return leaf.Value() }
`,
		"top/top.go": `package top

import "example.test/graph/mid"

func Value() int { return mid.Value() }
`,
		"other/other.go": "package other\n\nfunc Value() int { return 1 }\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "leaf/leaf.go", "package leaf\n\nfunc Value() int { return 2 }\n")

	analysis := analyze(t, repository, changeimpact.Options{Base: base, MaxPackages: 200})
	got := make([]string, 0, len(analysis.Impact.Packages))
	for _, pkg := range analysis.Impact.Packages {
		got = append(got, pkg.ID+":"+strings.Join(pkg.Reasons, ",")+":"+string(rune('0'+pkg.Distance)))
	}
	want := []string{
		"example.test/graph/leaf:changed_source:0",
		"example.test/graph/mid:reverse_import:1",
		"example.test/graph/top:reverse_import:2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impact = %v, want %v", got, want)
	}
	if !analysis.Complete || analysis.ObservedPackages != 3 {
		t.Fatalf("completion = %v/%d, want true/3", analysis.Complete, analysis.ObservedPackages)
	}
}

func TestImpactNeverTruncatesOversizedClosure(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":       "module example.test/graph\n\ngo 1.25.0\n",
		"leaf/leaf.go": "package leaf\n\nfunc Value() int { return 1 }\n",
		"mid/mid.go":   "package mid\n\nimport \"example.test/graph/leaf\"\n\nvar Value = leaf.Value\n",
		"top/top.go":   "package top\n\nimport \"example.test/graph/mid\"\n\nvar Value = mid.Value\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "leaf/leaf.go", "package leaf\n\nfunc Value() int { return 2 }\n")

	analysis := analyze(t, repository, changeimpact.Options{Base: base, MaxPackages: 2})
	if analysis.Complete || analysis.ObservedPackages != 3 || len(analysis.Packages) != 3 {
		t.Fatalf("bounded analysis = complete:%v observed:%d retained:%d, want false/3/3", analysis.Complete, analysis.ObservedPackages, len(analysis.Packages))
	}
	if len(analysis.Uncertainties) != 1 || analysis.Uncertainties[0].Code != "package_limit" {
		t.Fatalf("uncertainties = %#v, want package_limit", analysis.Uncertainties)
	}
}

func TestImpactMapsEmbeddedAndModuleMetadata(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":          "module example.test/embed\n\ngo 1.25.0\n",
		"assets/data.txt": "before\n",
		"assets/embed.go": "package assets\n\nimport \"embed\"\n\n//go:embed data.txt\nvar _ embed.FS\n",
		"consumer/use.go": "package consumer\n\nimport _ \"example.test/embed/assets\"\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "assets/data.txt", "after\n")

	analysis := analyze(t, repository, changeimpact.Options{Base: base})
	if got := analysis.Impact.Packages; len(got) != 2 || got[0].Reasons[0] != "embedded_file" || got[1].Reasons[0] != "reverse_import" {
		t.Fatalf("embedded impact = %#v", got)
	}

	writeFile(t, repository, "go.mod", "module example.test/embed\n\ngo 1.25.0\n\n// dependency policy changed\n")
	analysis = analyze(t, repository, changeimpact.Options{Base: base})
	for _, pkg := range analysis.Impact.Packages {
		if !contains(pkg.Reasons, "module_metadata") {
			t.Fatalf("package %#v has no module_metadata reason", pkg)
		}
		if pkg.Distance != 0 {
			t.Fatalf("metadata package distance = %d, want 0", pkg.Distance)
		}
	}
}

func TestImpactLoadsEveryActiveWorkModule(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.work":      "go 1.25.0\n\nuse (\n\t./one\n\t./two\n)\n",
		"one/go.mod":   "module example.test/one\n\ngo 1.25.0\n",
		"one/leaf.go":  "package one\n\nfunc Value() int { return 1 }\n",
		"two/go.mod":   "module example.test/two\n\ngo 1.25.0\n",
		"two/use.go":   "package two\n\nimport \"example.test/one\"\n\nvar Value = one.Value\n",
		"two/other.go": "package two\n\nconst Other = true\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "one/leaf.go", "package one\n\nfunc Value() int { return 2 }\n")

	analysis := analyze(t, repository, changeimpact.Options{Base: base})
	got := make([]string, 0, len(analysis.Impact.Packages))
	for _, pkg := range analysis.Impact.Packages {
		got = append(got, pkg.ID)
	}
	if want := []string{"example.test/one", "example.test/two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace impact = %v, want %v", got, want)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
