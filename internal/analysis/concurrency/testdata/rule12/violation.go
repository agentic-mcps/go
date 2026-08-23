package rule12

import "sync"

func Violation() { // VIOLATION: concurrency-12
	var wg sync.WaitGroup
	errs := make(chan error)
	go func() { errs <- nil }()
	go func() { errs <- nil }()
	wg.Wait()
}
