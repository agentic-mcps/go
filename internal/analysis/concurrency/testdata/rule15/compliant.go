package rule15

import "sync/atomic"

type CompliantState struct {
	a atomic.Int64
	b atomic.Int64
}

func (s *CompliantState) Compliant() { s.a.Add(1) }
