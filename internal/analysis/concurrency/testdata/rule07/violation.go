package rule07

import "sync"

func Violation(mu *sync.Mutex) {
	mu.Lock() // VIOLATION: concurrency-07
	work()
	defer mu.Unlock()
}

func work() {}
