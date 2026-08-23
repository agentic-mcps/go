package rule20

func Compliant(items []int) { // COMPLIANT: concurrency-20
	for range items {
		func() { defer cleanupCompliant() }()
	}
	for range []int{1, 2} {
		defer cleanupCompliant()
	}
	for range 8 {
		defer cleanupCompliant()
	}
}

func cleanupCompliant() {}
