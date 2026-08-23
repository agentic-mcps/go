package rule05

func ProcessAsyncSignal() <-chan struct{} { // COMPLIANT: concurrency-05
	done := make(chan struct{}, 1)
	go func() { done <- struct{}{} }()
	return done
}
