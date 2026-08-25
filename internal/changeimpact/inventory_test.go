package changeimpact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodePackageRecords(t *testing.T) {
	input := strings.NewReader(`{"ImportPath":"example.test/p","Dir":"/workspace/p","GoFiles":["p.go"],"Module":{"Path":"example.test","Dir":"/workspace"}}
{"ImportPath":"example.test/q","Dir":"/workspace/q","Error":{"Err":"build constraints exclude all Go files"}}
`)
	if _, err := decodePackageRecords(input); err == nil || !strings.Contains(err.Error(), "build constraints") {
		t.Fatalf("decodePackageRecords error = %v, want package error", err)
	}
}

func TestDecodePackageRecordsPreservesInventoryFields(t *testing.T) {
	input := strings.NewReader(`{"ImportPath":"example.test/p","Dir":"/workspace/p","GoFiles":["p.go"],"CgoFiles":["c.go"],"IgnoredGoFiles":["ignored.go"],"TestGoFiles":["p_test.go"],"XTestGoFiles":["p_external_test.go"],"EmbedFiles":["data.txt"],"TestEmbedFiles":["testdata.txt"],"XTestEmbedFiles":["xtestdata.txt"],"Imports":["fmt"],"TestImports":["testing"],"XTestImports":["example.test/p"],"Module":{"Path":"example.test","Dir":"/workspace"}}
`)
	records, err := decodePackageRecords(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ModulePath != "example.test" || len(records[0].XTestImports) != 1 || len(records[0].EmbedFiles) != 1 {
		t.Fatalf("decoded inventory = %#v", records)
	}
}

func TestWorkModuleUsesSortsAndResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.work")
	if err := os.WriteFile(path, []byte("go 1.25\n\nuse (\n\t./two\n\t./one\n)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uses, err := workModuleUses(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "one"), filepath.Join(dir, "two")}
	if strings.Join(uses, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("uses = %v, want %v", uses, want)
	}
}
