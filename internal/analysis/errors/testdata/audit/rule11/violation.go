package rule11

func MustParse() int { return 1 }
func Bad()           { MustParse() } // VIOLATION: errors-11
