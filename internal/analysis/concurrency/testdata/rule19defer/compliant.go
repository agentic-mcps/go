package rule19defer

func Compliant(xs []int) {
	for _, x := range xs {
		defer func(value int) { _ = value }(x)
	}
}
