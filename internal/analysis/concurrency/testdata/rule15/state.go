package rule15

import "sync/atomic"

type State struct {
	a atomic.Int64
	b atomic.Int64
}
