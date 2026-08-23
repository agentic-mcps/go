package rule19

func Compliant(xs []int) {
	for _, x := range xs {
		x := x
		go func() { _ = x }() /* COMPLIANT: concurrency-19 */
	}
}
