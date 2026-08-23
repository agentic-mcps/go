//go:build race

package testingfixture

import "testing"

var racedValue int

func TestRace(t *testing.T) {
	done := make(chan struct{})
	go func() {
		racedValue = 1
		close(done)
	}()
	racedValue = 2
	<-done
}
