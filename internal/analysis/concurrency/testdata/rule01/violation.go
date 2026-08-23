package rule01

func Violation() {
	go work() // VIOLATION: concurrency-01
}

func work() {}
