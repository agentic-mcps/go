package gopls

import (
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// Position is a zero-based LSP position whose Character is measured in UTF-16
// code units, not bytes or Unicode code points.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// PositionForOffset converts a UTF-8 byte boundary into an LSP position.
func PositionForOffset(contents []byte, offset int) (Position, error) {
	if !utf8.Valid(contents) {
		return Position{}, fmt.Errorf("source is not valid UTF-8")
	}
	if offset < 0 || offset > len(contents) {
		return Position{}, fmt.Errorf("byte offset %d is outside source", offset)
	}
	position := Position{}
	for index := 0; index < offset; {
		r, size := utf8.DecodeRune(contents[index:])
		if index+size > offset {
			return Position{}, fmt.Errorf("byte offset %d splits a UTF-8 encoding", offset)
		}
		index += size
		if r == '\n' {
			position.Line++
			position.Character = 0
			continue
		}
		position.Character += utf16.RuneLen(r)
	}
	return position, nil
}

// OffsetForPosition converts an LSP UTF-16 position into a UTF-8 byte boundary.
func OffsetForPosition(contents []byte, position Position) (int, error) {
	if !utf8.Valid(contents) {
		return 0, fmt.Errorf("source is not valid UTF-8")
	}
	if position.Line < 0 || position.Character < 0 {
		return 0, fmt.Errorf("negative LSP position %#v", position)
	}
	line, character := 0, 0
	for index := 0; index < len(contents); {
		if line == position.Line && character == position.Character {
			return index, nil
		}
		r, size := utf8.DecodeRune(contents[index:])
		if r == '\n' {
			if line == position.Line {
				return 0, fmt.Errorf("UTF-16 character %d is outside line %d", position.Character, position.Line)
			}
			line++
			character = 0
			index += size
			continue
		}
		next := character + utf16.RuneLen(r)
		if line == position.Line && position.Character > character && position.Character < next {
			return 0, fmt.Errorf("UTF-16 character %d splits a surrogate pair", position.Character)
		}
		character = next
		index += size
	}
	if line == position.Line && character == position.Character {
		return len(contents), nil
	}
	return 0, fmt.Errorf("LSP position %#v is outside source", position)
}
