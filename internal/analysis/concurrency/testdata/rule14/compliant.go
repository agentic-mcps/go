package rule14

import "sync"

type CompliantItem struct{}

type ItemWithoutReset struct{}

func (i *CompliantItem) Reset()                {}
func Compliant(p *sync.Pool, i *CompliantItem) { i.Reset(); p.Put(i) }
func NotResettable(p *sync.Pool)               { p.Put(&ItemWithoutReset{}) }
