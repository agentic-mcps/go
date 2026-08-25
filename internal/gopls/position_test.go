package gopls

import "testing"

func TestUTF16PositionRoundTrip(t *testing.T) {
	contents := []byte("a🙂b\né")
	for _, test := range []struct {
		offset int
		want   Position
	}{
		{offset: 0, want: Position{Line: 0, Character: 0}},
		{offset: 1, want: Position{Line: 0, Character: 1}},
		{offset: 5, want: Position{Line: 0, Character: 3}},
		{offset: 6, want: Position{Line: 0, Character: 4}},
		{offset: 7, want: Position{Line: 1, Character: 0}},
		{offset: 9, want: Position{Line: 1, Character: 1}},
	} {
		position, err := PositionForOffset(contents, test.offset)
		if err != nil {
			t.Fatalf("PositionForOffset(%d): %v", test.offset, err)
		}
		if position != test.want {
			t.Fatalf("PositionForOffset(%d) = %#v, want %#v", test.offset, position, test.want)
		}
		offset, err := OffsetForPosition(contents, position)
		if err != nil {
			t.Fatalf("OffsetForPosition(%#v): %v", position, err)
		}
		if offset != test.offset {
			t.Fatalf("round-trip offset = %d, want %d", offset, test.offset)
		}
	}
}

func TestUTF16PositionsRejectInvalidBoundaries(t *testing.T) {
	contents := []byte("a🙂b")
	for _, offset := range []int{-1, 2, 3, 4, len(contents) + 1} {
		if _, err := PositionForOffset(contents, offset); err == nil {
			t.Fatalf("PositionForOffset(%d) succeeded", offset)
		}
	}
	for _, position := range []Position{
		{Line: -1, Character: 0},
		{Line: 0, Character: -1},
		{Line: 0, Character: 2},
		{Line: 0, Character: 5},
		{Line: 1, Character: 0},
	} {
		if _, err := OffsetForPosition(contents, position); err == nil {
			t.Fatalf("OffsetForPosition(%#v) succeeded", position)
		}
	}
}

func TestUTF16PositionsRejectInvalidUTF8(t *testing.T) {
	contents := []byte{0xff}
	if _, err := PositionForOffset(contents, 1); err == nil {
		t.Fatal("PositionForOffset accepted invalid UTF-8")
	}
	if _, err := OffsetForPosition(contents, Position{}); err == nil {
		t.Fatal("OffsetForPosition accepted invalid UTF-8")
	}
}
