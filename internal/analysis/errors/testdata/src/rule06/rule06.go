package rule06

import (
	"errors"
	"fmt"
)

func Bad(err error) error {
	return fmt.Errorf("failed to fetch user: %w", err) // want "starts with a \\\"failed to\\\" prefix"
}
func AlsoBad() error         { return errors.New("Failed to connect") } // want "starts with a \\\"failed to\\\" prefix" "starts with an uppercase letter"
func Good(err error) error   { return fmt.Errorf("fetching user: %w", err) }
func Dynamic(s string) error { return fmt.Errorf(s) }
