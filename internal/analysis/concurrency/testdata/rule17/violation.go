package rule17

import "sync"

func Violation(a, b *sync.Mutex) { a.Lock(); b.Lock() } // VIOLATION: concurrency-17
