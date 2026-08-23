package rule03

func Bad(err error) int {
	if err != nil {
		return 0
	} else { // want "nests the happy path"
		return 1
	}
}

func Good(err error) int {
	if err != nil {
		return 0
	} else if err == nil {
		return 1
	}
	return 1
}
