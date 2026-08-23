package rule20

func Violation(items []int) {
	for range items {
		defer cleanup() // VIOLATION: concurrency-20
	}
}

func cleanup() {}
