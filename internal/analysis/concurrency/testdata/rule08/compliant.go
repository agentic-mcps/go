package rule08

import "sync"

type Compliant struct { // COMPLIANT: concurrency-08
	mu sync.Mutex
}
