package rule10

func Good() {
	panic := func(any) {}
	panic(nil)
}
