package rule17

import "strings"

func Bad(err error) bool { return strings.Contains(err.Error(), "timeout") } // VIOLATION: errors-17
