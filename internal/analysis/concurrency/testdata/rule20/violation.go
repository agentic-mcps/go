package rule20

func Violation(items []int) {
	for range items {
		defer cleanup() // VIOLATION: concurrency-20
	}
}

func Reassigned() {
	bounded := []int{1, 2}
	bounded = []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	for range bounded {
		defer cleanup() // VIOLATION: concurrency-20
	}
}

func cleanup() {}
