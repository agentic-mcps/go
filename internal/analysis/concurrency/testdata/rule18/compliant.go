package rule18

import "time"

func Compliant() { // COMPLIANT: concurrency-18
	t := time.NewTicker(time.Second)
	defer t.Stop()
}
