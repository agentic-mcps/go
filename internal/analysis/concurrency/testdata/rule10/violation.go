package rule10

import "sync"

type Violation struct { // VIOLATION: concurrency-10
	mu sync.Mutex
}
