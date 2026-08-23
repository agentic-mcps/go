package rule07

import (
	"errors"
	"fmt"
)

func Upper() error         { return errors.New("Invalid value") }         // want "starts with an uppercase letter"
func Punct() error         { return errors.New("invalid value!") }        // want "ends with punctuation"
func Acronym() error       { return fmt.Errorf("invalid HTTP response") } // want "contains a capitalized acronym"
func Good(err error) error { return fmt.Errorf("invalid value: %w", err) }
