package rule03

func Compliant() {
	_ = make(chan int, 1) // COMPLIANT: concurrency-03
}
