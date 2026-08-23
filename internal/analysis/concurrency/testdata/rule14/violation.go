package rule14

import "sync"

type Item struct{}

func (i *Item) Reset()                {}
func Violation(p *sync.Pool, i *Item) { p.Put(i) } // VIOLATION: concurrency-14
