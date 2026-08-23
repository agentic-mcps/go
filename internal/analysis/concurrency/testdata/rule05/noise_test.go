package rule05

func testOnlyAsync() { go func() {}() }
