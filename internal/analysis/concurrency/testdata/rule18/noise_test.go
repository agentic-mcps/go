package rule18

import "time"

func testOnly() {
	for {
		select {
		case <-time.After(time.Second):
		}
	}
}
