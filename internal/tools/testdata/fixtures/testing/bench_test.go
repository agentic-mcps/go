package testingfixture

import (
	"fmt"
	"testing"
)

func BenchmarkTrivial(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%d", i)
	}
}
