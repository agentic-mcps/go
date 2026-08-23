package parser

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDecodeTestJSONDeliversValidEventsInOrder(t *testing.T) {
	input := strings.Join([]string{
		`{"Action":"run","Package":"./pkg","Test":"TestOne"}`,
		`{"Action":"output","Package":"./pkg","Test":"TestOne","Output":"hello\n"}`,
		`{"Action":"pass","Package":"./pkg","Test":"TestOne","Elapsed":0.25}`,
	}, "\n") + "\n"
	var got []string
	stats, err := DecodeTestJSON(strings.NewReader(input), func(event TestEvent) error {
		got = append(got, event.Action)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "run,output,pass"; strings.Join(got, ",") != want {
		t.Fatalf("actions = %q, want %q", strings.Join(got, ","), want)
	}
	if stats != (Stats{Valid: 3}) {
		t.Fatalf("stats = %#v, want valid-only stats", stats)
	}
}

func TestDecodeTestJSONSkipsMalformedLines(t *testing.T) {
	input := strings.Join([]string{
		"not json",
		`{"Package":"./pkg"}`,
		`{"Action":"pass"}`,
		`{"Action":"future","Package":"./pkg"}`,
		`{"Action":"pass","Package":"./pkg"}`,
		`{"Action":`,
	}, "\n") + "\n"
	var count int
	stats, err := DecodeTestJSON(strings.NewReader(input), func(TestEvent) error { count++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || stats != (Stats{Valid: 1, Malformed: 5}) {
		t.Fatalf("count=%d stats=%#v, want one valid and five malformed", count, stats)
	}
}

func TestDecodeTestJSONNoValidEvents(t *testing.T) {
	stats, err := DecodeTestJSON(strings.NewReader("bad\n"), func(TestEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "no valid events") {
		t.Fatalf("err = %v, want no-valid-events error", err)
	}
	if stats.Malformed != 1 {
		t.Fatalf("stats = %#v, want one malformed line", stats)
	}
}

func TestDecodeTestJSONRejectsOversizedEvent(t *testing.T) {
	input := strings.NewReader(fmt.Sprintf(`{"Action":"output","Output":"%s"}\n`, strings.Repeat("x", maxTestJSONEvent)))
	_, err := DecodeTestJSON(input, func(TestEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("err = %v, want scanner size error", err)
	}
}

func TestDecodeTestJSONPropagatesCallbackError(t *testing.T) {
	want := errors.New("stop")
	stats, err := DecodeTestJSON(strings.NewReader(`{"Action":"run","Package":"./pkg"}`), func(TestEvent) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want callback error", err)
	}
	if stats.Valid != 1 {
		t.Fatalf("stats = %#v, want one valid event", stats)
	}
}
