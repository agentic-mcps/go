package rule12

import "strings"

func Bad(err error) int {
	if strings.Contains(err.Error(), "missing") { // VIOLATION: errors-12
		return 404
	}
	return 500
}
