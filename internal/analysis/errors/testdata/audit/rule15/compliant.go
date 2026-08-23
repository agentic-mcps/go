package rule15

func Good()  { go func() { defer func() { _ = recover() }() }() }
func named() {}
func Named() { go named() }
