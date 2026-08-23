package rule17

import "sync"

func Compliant(a, b *sync.Mutex) { // COMPLIANT: concurrency-17
	// lock order: a before b
	a.Lock()
	b.Lock()
}

type Gate struct{}

func (*Gate) Lock() {}

func CustomLockNames(a, b *Gate) {
	a.Lock()
	b.Lock()
}
