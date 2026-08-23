package rule18

import "time"

func Violation() {
	for {
		select {
		case <-time.After(time.Second): // VIOLATION: concurrency-18
			return
		}
	}
}
