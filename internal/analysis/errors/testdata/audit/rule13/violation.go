package rule13

import "fmt"

func Bad(a, b error) error { return fmt.Errorf("both: %w and %w", a, b) } // VIOLATION: errors-13
