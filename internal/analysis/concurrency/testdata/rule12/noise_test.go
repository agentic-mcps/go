package rule12

import "sync"

func testOnly() {
	var wg sync.WaitGroup
	errs := make(chan error)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- nil }()
	go func() { defer wg.Done(); errs <- nil }()
}
