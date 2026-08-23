package rule03

func Bad(err error) int {
	if err != nil {
		return 0
	} else { // VIOLATION: errors-03
		return 1
	}
}
