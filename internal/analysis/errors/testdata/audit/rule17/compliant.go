package rule17

import "strings"

func Good(err error) int {
	if strings.HasPrefix(err.Error(), "timeout") {
		return 1
	}
	return 0
}
