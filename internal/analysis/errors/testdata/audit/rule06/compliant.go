package rule06

import "fmt"

func Good(err error) error { return fmt.Errorf("fetching user: %w", err) }
