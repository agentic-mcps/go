package rule18ticker

import "time"

func Compliant() {
	t := time.NewTimer(time.Second)
	defer t.Stop()
}
