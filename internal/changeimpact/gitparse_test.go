package changeimpact

import (
	"reflect"
	"testing"
)

func TestParseRawChanges(t *testing.T) {
	input := []byte(":100644 100644 abc def M\x00dir/file name.go\x00:100644 000000 abc 000000 D\x00gone.go\x00:000000 100644 000000 fed A\x00new.go\x00:100644 100644 aaa bbb R087\x00old name.go\x00new name.go\x00")
	want := []RawChange{
		{OldMode: "100644", NewMode: "100644", OldOID: "abc", NewOID: "def", Status: 'M', Path: "dir/file name.go"},
		{OldMode: "100644", NewMode: "000000", OldOID: "abc", NewOID: "000000", Status: 'D', Path: "gone.go"},
		{OldMode: "000000", NewMode: "100644", OldOID: "000000", NewOID: "fed", Status: 'A', Path: "new.go"},
		{OldMode: "100644", NewMode: "100644", OldOID: "aaa", NewOID: "bbb", Status: 'R', Score: 87, OldPath: "old name.go", Path: "new name.go"},
	}
	got, err := ParseRawChanges(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseRawChangesErrors(t *testing.T) {
	for _, input := range [][]byte{
		[]byte(":100644 100644 a b M\x00"),
		[]byte(":100644 100644 a b R90\x00old\x00"),
		[]byte(":100644 100644 a b Z\x00x\x00"),
		[]byte(":100644 100644 a b R101\x00old\x00new\x00"),
	} {
		if _, err := ParseRawChanges(input); err == nil {
			t.Fatalf("ParseRawChanges(%q) succeeded", input)
		}
	}
}

func TestParseHunks(t *testing.T) {
	input := []byte("diff --git a/a.go b/a.go\n@@ -0,0 +1,3 @@\n+x\n@@ -9 +12 @@\n-x\n+y\n@@ -20,0 +21,0 @@\n")
	want := []Hunk{{0, 0, 1, 3}, {9, 1, 12, 1}, {20, 0, 21, 0}}
	got, err := ParseHunks(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseHunksErrors(t *testing.T) {
	if _, err := ParseHunks([]byte("@@ -x +1 @@\n")); err == nil {
		t.Fatal("malformed hunk accepted")
	}
}
