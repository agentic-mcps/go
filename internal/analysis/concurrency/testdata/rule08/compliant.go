package rule08

import "sync"

// Compliant is safe for concurrent use.
type Compliant struct { // COMPLIANT: concurrency-08
	mu sync.Mutex
}

// Mutex is an intentional synchronization wrapper.
type Mutex struct {
	sync.Mutex
}
