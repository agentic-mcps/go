package rule05

func ProcessAsync() { // VIOLATION: concurrency-05
	go work()
}

func work() {}
