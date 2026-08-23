package testing

import "sync"

type Fixture struct {
	mu sync.Mutex
}
