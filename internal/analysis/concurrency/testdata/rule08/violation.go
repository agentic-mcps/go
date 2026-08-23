package rule08

import "sync"

type Violation struct {
	sync.Mutex // VIOLATION: concurrency-08
}
