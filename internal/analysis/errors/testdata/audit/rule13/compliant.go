package rule13

import "fmt"

func Good(err error) error { return fmt.Errorf("literal %%w and wrapped: %w", err) }
