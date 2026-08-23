package rule18ticker

import "time"

func Violation() {
	_ = time.Second
	t := time.NewTicker(time.Second) // VIOLATION: concurrency-18
	_ = t
}
