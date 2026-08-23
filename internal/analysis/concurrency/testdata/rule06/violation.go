package rule06

import "context"

func Violation() {
	go func() { _ = context.Background() }() // VIOLATION: concurrency-06
}
