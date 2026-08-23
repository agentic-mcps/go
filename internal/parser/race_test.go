package parser

import "testing"

const raceFixture = `WARNING: DATA RACE
Write at 0x00c0000a4010 by goroutine 7:
  example.com/pkg.Write()
      /work/write.go:42 +0x1a3

Previous read at 0x00c0000a4010 by goroutine 6:
  example.com/pkg.Read()
      /work/read.go:38 +0x27

Goroutine 7 (running) created at:
  example.com/pkg.Start()
      /work/start.go:30 +0x9c

Goroutine 6 (finished) created at:
  example.com/pkg.Launch()
      /work/launch.go:26 +0x4f
==================`

func TestParseRaceBlock(t *testing.T) {
	got := Parse(raceFixture)
	if got.RawBlocksFound != 1 || len(got.Conflicts) != 1 {
		t.Fatalf("got %#v", got)
	}
	c := got.Conflicts[0]
	if c.Current.Kind != "write" || c.Current.GoroutineID != 7 || c.Current.Location.Line != 42 {
		t.Fatalf("current %#v", c.Current)
	}
	if c.Previous.Kind != "read" || c.Previous.Location.File != "/work/read.go" {
		t.Fatalf("previous %#v", c.Previous)
	}
	if len(c.GoroutineCreation) != 2 || c.GoroutineCreation[1].State != "finished" {
		t.Fatalf("creation %#v", c.GoroutineCreation)
	}
}

func TestParseMultipleAndNoRace(t *testing.T) {
	got := Parse(raceFixture + "\r\n" + raceFixture)
	if got.RawBlocksFound != 2 || len(got.Conflicts) != 2 {
		t.Fatalf("got %#v", got)
	}
	got = Parse("ok\nno race\n")
	if got.RawBlocksFound != 0 || got.Conflicts == nil || len(got.Conflicts) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestParsePartialEmitsBoundedUnparsed(t *testing.T) {
	got := Parse("WARNING: DATA RACE\nWrite at 0x1 by goroutine 2:\n  pkg.F()\n")
	if len(got.Conflicts) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got.Conflicts[0].Current.Function != unparsedRaceMarker {
		t.Fatalf("got %#v", got)
	}
	if got.Conflicts[0].GoroutineCreation == nil {
		t.Fatal("GoroutineCreation is nil, want an empty slice")
	}
}
