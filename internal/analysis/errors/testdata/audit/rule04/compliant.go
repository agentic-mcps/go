package rule04

import "fmt"

func Good(err error) error { return fmt.Errorf("request: %w", err) }
