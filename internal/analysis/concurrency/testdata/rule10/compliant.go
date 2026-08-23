package rule10

import "sync"

// Compliant is safe for concurrent use.
type Compliant struct { // COMPLIANT: concurrency-10
	mu sync.Mutex
}
