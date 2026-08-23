package rule20

func Compliant(items []int) { // COMPLIANT: concurrency-20
	for range items {
		func() { defer cleanupCompliant() }()
	}
}

func cleanupCompliant() {}
