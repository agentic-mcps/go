package rule01

func Bad() (error, int) { return nil, 0 } // VIOLATION: errors-01
