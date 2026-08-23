package rule19

func Violation(xs []int) {
	for i, x := range xs {
		x := x
		go func() { _, _ = i, x }() /* VIOLATION: concurrency-19 */
	}
}
