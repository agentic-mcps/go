package testingfixture

import (
	"sync/atomic"
	"testing"
)

var flakeRun atomic.Uint32

func TestFlaky(t *testing.T) {
	if flakeRun.Add(1)%2 == 0 {
		t.Error("intentional alternating failure")
	}
}
