package testingfixture

import "testing"

func TestPass(t *testing.T) {
	t.Log("passing output")
}

func TestSkip(t *testing.T) {
	t.Skip("intentional skip")
}

func TestFail(t *testing.T) {
	t.Log("failure output")
	t.Error("intentional failure")
}
