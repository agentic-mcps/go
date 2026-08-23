package failing

import "testing"

func TestFail(t *testing.T) {
	t.Log("failure output")
	t.Error("intentional failure")
}
