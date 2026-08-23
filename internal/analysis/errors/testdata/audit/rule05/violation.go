package rule05

func Bad(err error) (int, error) {
	if err != nil { // VIOLATION: errors-05
		return 0, err
	}
	return 1, nil
}
