package rule07

import "sync"

func Compliant(mu *sync.Mutex) { // COMPLIANT: concurrency-07
	mu.Lock()
	defer mu.Unlock()
}
