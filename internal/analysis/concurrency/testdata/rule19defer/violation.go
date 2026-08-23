package rule19defer

func Violation(xs []int) {
	for _, x := range xs {
		defer func() { _ = x }() // VIOLATION: concurrency-19
	}
}
