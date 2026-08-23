package rule01

func Bad() (error, int)              { return nil, 0 } // want "function Bad returns error"
func Good() (int, error)             { return 0, nil }
func Grouped() (x, y int, err error) { return 0, 0, nil }
