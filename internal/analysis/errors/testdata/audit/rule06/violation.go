package rule06

import "fmt"

func Bad(err error) error { return fmt.Errorf("failed to fetch: %w", err) } // VIOLATION: errors-06
