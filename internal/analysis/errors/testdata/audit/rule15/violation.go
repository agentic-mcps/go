package rule15

func Bad() { go func() { _ = 1 }() } // VIOLATION: errors-15
