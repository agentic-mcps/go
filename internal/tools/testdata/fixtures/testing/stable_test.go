package testingfixture

import "testing"

func TestPass(t *testing.T) {
	t.Log("passing output")
	if got := Indexed([]int{7}, 0); got != 7 {
		t.Fatalf("Indexed() = %d, want 7", got)
	}
}

func TestSkip(t *testing.T) {
	t.Skip("intentional skip")
}
