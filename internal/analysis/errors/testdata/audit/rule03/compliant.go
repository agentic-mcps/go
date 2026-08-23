package rule03

func Good(err error) int {
	if err != nil {
		return 0
	}
	return 1
}
