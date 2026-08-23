package rule03

func Violation() {
	_ = make(chan int, 100) // VIOLATION: concurrency-03
}
